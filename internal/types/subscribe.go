package types

type (
	SubscribeRequest struct {
		Token  string
		Type   string
		UA     string
		Agent  string
		Params map[string]string
	}
	SubscribeResponse struct {
		Config  []byte
		Header  string
		Headers map[string]string
	}
)
