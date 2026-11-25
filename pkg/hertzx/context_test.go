package hertzx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
)

func TestShouldBindReadRequestsUseQueryWithJSONContentType(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			raw := app.NewContext(0)
			raw.Request.Header.SetMethod(method)
			raw.Request.Header.Set("Content-Type", "application/json")
			raw.Request.SetRequestURI("/list?page=2&enable=false&status=0&id[]=7&id[]=9")
			var request struct {
				Page   int     `form:"page"`
				Enable *bool   `form:"enable"`
				Status *int    `form:"status"`
				IDs    []int64 `form:"id"`
			}
			if err := NewContext(context.Background(), raw).ShouldBind(&request); err != nil {
				t.Fatal(err)
			}
			if request.Page != 2 || request.Enable == nil || *request.Enable || request.Status == nil || *request.Status != 0 || len(request.IDs) != 2 || request.IDs[0] != 7 || request.IDs[1] != 9 {
				t.Fatalf("query filters not bound: %+v", request)
			}
		})
	}
}

func TestRouterRejectsOutOfRangeRouteAndQueryParameters(t *testing.T) {
	engine := New()
	engine.GET("/plans/:plan_id", func(c *Context) {
		var path struct {
			PlanID uint16 `uri:"plan_id"`
		}
		var query struct {
			Rate float64 `form:"rate"`
		}
		if err := c.ShouldBindUri(&path); err != nil {
			c.String(http.StatusBadRequest, "invalid plan: %s", err)
			return
		}
		if err := c.ShouldBindQuery(&query); err != nil {
			c.String(http.StatusBadRequest, "invalid filter: %s", err)
			return
		}
		c.String(http.StatusOK, "plan %d at %.2f", path.PlanID, query.Rate)
	})
	for _, test := range []struct {
		name string
		path string
		code int
		body string
	}{
		{"valid", "/plans/7?rate=1.25", http.StatusOK, "plan 7 at 1.25"},
		{"overflow", "/plans/65536?rate=1.25", http.StatusBadRequest, "invalid plan: bind PlanID:"},
		{"malformed rate", "/plans/7?rate=abc", http.StatusBadRequest, "invalid filter: bind Rate:"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
			if response.Code != test.code || !strings.HasPrefix(response.Body.String(), test.body) {
				t.Fatalf("response = %d %q, want %d with prefix %q", response.Code, response.Body.String(), test.code, test.body)
			}
		})
	}
}

func TestShouldBindFormAndQuerySupportsEmbeddedPointersAndCollections(t *testing.T) {
	raw := app.NewContext(0)
	raw.Request.Header.SetMethod(http.MethodPost)
	raw.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	raw.Request.SetRequestURI("/search?ignored=1&labels[]=one&labels[]=two")
	raw.Request.SetBodyString("name=fixture&enabled=true&count=4&ratio=1.5&nested=7")
	ctx := NewContext(context.Background(), raw)
	var request struct {
		Name    string   `form:"name"`
		Enabled *bool    `form:"enabled"`
		Count   int32    `form:"count"`
		Ratio   float64  `form:"ratio"`
		Labels  []string `form:"labels"`
		Nested  struct {
			ID int64 `form:"nested"`
		}
	}
	if err := ctx.ShouldBind(&request); err != nil {
		t.Fatal(err)
	}
	if request.Name != "fixture" || request.Enabled == nil || !*request.Enabled || request.Count != 4 || request.Ratio != 1.5 || request.Nested.ID != 7 {
		t.Fatalf("form values = %+v", request)
	}
	var query struct {
		Labels []string `form:"labels"`
	}
	if err := ctx.ShouldBindQuery(&query); err != nil || strings.Join(query.Labels, ",") != "one,two" {
		t.Fatalf("query collection = %+v, %v", query, err)
	}
	if got := ctx.GetHeader("Content-Type"); got != "application/x-www-form-urlencoded" {
		t.Fatalf("content type = %q", got)
	}
}

func TestContextResponseWriterAndValuesStayInSync(t *testing.T) {
	raw := app.NewContext(0)
	raw.Request.Header.SetMethod(http.MethodGet)
	raw.Request.SetRequestURI("/fixture")
	ctx := NewContext(context.Background(), raw)
	ctx.Set("number", 42)
	if got := ctx.GetString("number"); got != "42" || ctx.Value("missing") != nil {
		t.Fatalf("context values = %q, %v", got, ctx.Value("missing"))
	}
	if ctx.Error(nil) != nil || ctx.Error(errors.New("first")).Error() != "first" || ctx.Errors.Last().Error() != "first" || ctx.Errors.String() != "first" {
		t.Fatalf("error chain = %v", ctx.Errors)
	}
	ctx.Header("X-Fixture", "yes")
	ctx.JSON(http.StatusCreated, map[string]string{"ok": "yes"})
	ctx.flush()
	if ctx.Writer.Status() != http.StatusCreated || !ctx.Writer.Written() || ctx.Writer.Size() == 0 || string(raw.Response.Header.Peek("X-Fixture")) != "yes" {
		t.Fatalf("response state = status %d written %t size %d headers %q", ctx.Writer.Status(), ctx.Writer.Written(), ctx.Writer.Size(), raw.Response.Header.Peek("X-Fixture"))
	}
	ctx.Status(http.StatusNoContent)
	if ctx.Writer.Status() != http.StatusNoContent {
		t.Fatalf("status was not forwarded: %d", ctx.Writer.Status())
	}
}

func TestContextRequestDelegatesCancellationAndRecovery(t *testing.T) {
	base, cancel := context.WithCancel(context.Background())
	raw := app.NewContext(0)
	raw.Request.Header.SetMethod(http.MethodGet)
	raw.Request.SetRequestURI("/fixture")
	ctx := NewContext(base, raw)
	deadline, ok := ctx.Deadline()
	if ok || !deadline.IsZero() || ctx.Done() == nil || ctx.Err() != nil {
		t.Fatalf("initial context state = deadline %v/%t done %v err %v", deadline, ok, ctx.Done(), ctx.Err())
	}
	cancel()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("request context did not cancel")
	}
	if ctx.Err() != context.Canceled {
		t.Fatalf("context error = %v", ctx.Err())
	}
	engine := New()
	engine.Use(Recovery())
	engine.GET("/panic", func(*Context) { panic("fixture") })
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if response.Code != http.StatusInternalServerError || response.Body.String() != "Internal Server Error" {
		t.Fatalf("recovery response = %d %q", response.Code, response.Body.String())
	}
}

func TestEngineAdaptersExposeHTTPBehavior(t *testing.T) {
	engine := New()
	engine.Use(func(c *Context) {
		c.Set("middleware", "visited")
		c.Next()
	})
	engine.GET("/get", func(c *Context) {
		if c.GetString("middleware") != "visited" || c.Param("missing") != "" || c.GetString("missing") != "" {
			c.String(http.StatusInternalServerError, "middleware state missing")
			return
		}
		c.Header("X-Route", "get")
		c.String(http.StatusOK, "get %s", c.FullPath())
	})
	engine.POST("/post", func(c *Context) {
		SyncRequestBody(c)
		SyncRequestURI(c)
		if !MethodPost(c) || RequestContext(c) == nil || HertzRequest(c) == nil || string(HertzRequest(c).Body()) != `{"name":"fixture"}` {
			c.String(http.StatusInternalServerError, "request adapter failed")
			return
		}
		c.JSON(http.StatusAccepted, map[string]string{"ok": "post"})
	})
	engine.PUT("/put", func(c *Context) { c.Redirect(http.StatusFound, "/get") })
	engine.DELETE("/delete", func(c *Context) { c.Status(http.StatusNoContent); c.Abort() })
	engine.Any("/any", func(c *Context) { c.String(http.StatusOK, "%s", string(c.Request.Method)) })
	engine.GET("/writer", func(c *Context) {
		c.Writer.Header().Set("X-Writer", "yes")
		c.Writer.WriteHeaderNow()
		_, _ = c.Writer.WriteString("writer")
		c.Writer.Flush()
		if c.Writer.Pusher() != nil {
			c.String(http.StatusInternalServerError, "unexpected pusher")
		}
		if _, _, err := c.Writer.Hijack(); !errors.Is(err, http.ErrNotSupported) {
			c.String(http.StatusInternalServerError, "unexpected hijack result")
		}
	})
	group := engine.Group("/group")
	group.Use(func(c *Context) { c.Set("group", "yes"); c.Next() })
	group.GET("/get", func(c *Context) { c.String(http.StatusOK, "%s", c.GetString("group")) })
	nested := group.Group("/nested")
	nested.POST("/post", func(c *Context) { c.String(http.StatusOK, "nested") })
	group.PUT("/put", func(c *Context) { c.String(http.StatusOK, "group-put") })
	group.DELETE("/delete", func(c *Context) { c.String(http.StatusOK, "group-delete") })
	group.Any("/any", func(c *Context) { c.String(http.StatusOK, "%s", "group-any") })
	engine.NoRoute(func(c *Context) { c.String(http.StatusNotFound, "missing") })

	for _, tc := range []struct {
		method string
		path   string
		body   string
		code   int
		want   string
	}{
		{http.MethodGet, "/get", "", http.StatusOK, "get /get"},
		{http.MethodPost, "/post", `{"name":"fixture"}`, http.StatusAccepted, `{"ok":"post"}`},
		{http.MethodPut, "/put", "", http.StatusFound, ""},
		{http.MethodDelete, "/delete", "", http.StatusNoContent, ""},
		{http.MethodPatch, "/any", "", http.StatusOK, http.MethodPatch},
		{http.MethodGet, "/writer", "", http.StatusOK, "writer"},
		{http.MethodGet, "/group/get", "", http.StatusOK, "yes"},
		{http.MethodPost, "/group/nested/post", "", http.StatusOK, "nested"},
		{http.MethodPut, "/group/put", "", http.StatusOK, "group-put"},
		{http.MethodDelete, "/group/delete", "", http.StatusOK, "group-delete"},
		{http.MethodOptions, "/group/any", "", http.StatusOK, "group-any"},
		{http.MethodGet, "/missing", "", http.StatusNotFound, "missing"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, req)
			if response.Code != tc.code || (tc.want != "" && response.Body.String() != tc.want) {
				t.Fatalf("response = %d %q, want %d %q", response.Code, response.Body.String(), tc.code, tc.want)
			}
		})
	}
	if response := func() *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/get", nil))
		return response
	}(); response.Header().Get("X-Route") != "get" {
		t.Fatalf("route header = %q", response.Header().Get("X-Route"))
	}
	if response := func() *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/put", nil))
		return response
	}(); response.Header().Get("Location") != "/get" {
		t.Fatalf("redirect location = %q", response.Header().Get("Location"))
	}
	if response := func() *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/writer", nil))
		return response
	}(); response.Header().Get("X-Writer") != "yes" {
		t.Fatalf("writer header = %q", response.Header().Get("X-Writer"))
	}
	_ = Default()
}

func TestShouldBindJSONPreservesFalseAndZero(t *testing.T) {
	raw := app.NewContext(0)
	raw.Request.Header.SetMethod(http.MethodPut)
	raw.Request.Header.Set("Content-Type", "application/json")
	raw.Request.SetRequestURI("/update?enable=true&balance=100")
	raw.Request.SetBodyString(`{"user_id":"7","enable":false,"balance":0}`)
	var request struct {
		UserID  int64 `json:"user_id,string"`
		Enable  *bool `json:"enable"`
		Balance int64 `json:"balance"`
	}
	if err := NewContext(context.Background(), raw).ShouldBind(&request); err != nil {
		t.Fatal(err)
	}
	if request.UserID != 7 || request.Enable == nil || *request.Enable || request.Balance != 0 {
		t.Fatalf("JSON body lost explicit values: %+v", request)
	}
}
