package result

import (
	"context"
	stderrors "errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

func TestBuildHTTPResultSuccess(t *testing.T) {
	data := map[string]string{"status": "ok"}

	result := BuildHTTPResult(data, nil)

	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, result.StatusCode)
	}
	body, ok := result.Body.(*ResponseSuccessBean)
	if !ok {
		t.Fatalf("expected success body, got %T", result.Body)
	}
	if body.Code != http.StatusOK {
		t.Fatalf("expected code %d, got %d", http.StatusOK, body.Code)
	}
	if body.Msg != "success" {
		t.Fatalf("expected success message, got %q", body.Msg)
	}
	if !reflect.DeepEqual(body.Data, data) {
		t.Fatalf("expected data to be preserved")
	}
}

func TestBuildHTTPResultCodeError(t *testing.T) {
	err := errors.Wrap(xerr.NewErrCode(xerr.InvalidParams), "wrapped")

	result := BuildHTTPResult(nil, err)

	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, result.StatusCode)
	}
	body, ok := result.Body.(*ResponseErrorBean)
	if !ok {
		t.Fatalf("expected error body, got %T", result.Body)
	}
	if body.Code != xerr.InvalidParams {
		t.Fatalf("expected code %d, got %d", xerr.InvalidParams, body.Code)
	}
	if body.Msg != xerr.MapErrMsg(xerr.InvalidParams) {
		t.Fatalf("expected mapped message %q, got %q", xerr.MapErrMsg(xerr.InvalidParams), body.Msg)
	}
}

func TestBuildHTTPResultGenericError(t *testing.T) {
	result := BuildHTTPResult(nil, stderrors.New("boom"))

	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, result.StatusCode)
	}
	body, ok := result.Body.(*ResponseErrorBean)
	if !ok {
		t.Fatalf("expected error body, got %T", result.Body)
	}
	if body.Code != xerr.ERROR {
		t.Fatalf("expected code %d, got %d", xerr.ERROR, body.Code)
	}
	if body.Msg != "Internal Server Error" {
		t.Fatalf("expected internal server error, got %q", body.Msg)
	}
}

func TestBuildParamErrorResult(t *testing.T) {
	result := BuildParamErrorResult(stderrors.New("bad param"))

	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, result.StatusCode)
	}
	body, ok := result.Body.(*ResponseErrorBean)
	if !ok {
		t.Fatalf("expected error body, got %T", result.Body)
	}
	if body.Code != xerr.InvalidParams {
		t.Fatalf("expected code %d, got %d", xerr.InvalidParams, body.Code)
	}
	if body.Msg != "bad param" {
		t.Fatalf("expected param message, got %q", body.Msg)
	}
}

// HttpResult must attach a returned error to the request context so the logger
// middleware can record it. Without this the request is logged as a success and
// the backend error never shows up anywhere.
func TestHttpResultAttachesError(t *testing.T) {
	c := hertzx.NewContext(context.Background(), app.NewContext(0))

	err := errors.Wrap(xerr.NewErrCode(xerr.InvalidParams), "wrapped")
	HttpResult(c, nil, err)

	last := c.Errors.Last()
	if last == nil {
		t.Fatalf("expected the error to be attached to the context")
	}
	if last.Err != err {
		t.Fatalf("expected the original error to be attached, got %v", last.Err)
	}
}

func TestHttpResultSuccessAttachesNoError(t *testing.T) {
	c := hertzx.NewContext(context.Background(), app.NewContext(0))

	HttpResult(c, map[string]string{"status": "ok"}, nil)

	if c.Errors.Last() != nil {
		t.Fatalf("did not expect an error on success, got %v", c.Errors.Last())
	}
}
