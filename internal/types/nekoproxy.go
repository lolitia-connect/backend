package types

type NekoproxyConfig struct {
	APIURL      string `json:"api_url"`
	HasAPIToken bool   `json:"has_api_token"`
}

type UpdateNekoproxyConfigRequest struct {
	APIURL   string `json:"api_url" validate:"required,max=512"`
	APIToken string `json:"api_token,omitempty" validate:"max=512"`
}

type NekoproxySession struct {
	ID      string `json:"id"`
	Address string `json:"address"`
	Port    int    `json:"port"`
	Status  string `json:"status"`
	GroupID string `json:"group_id"`
}

type NekoproxyForward struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	ChainID    string             `json:"chain_id"`
	ChainName  string             `json:"chain_name"`
	Protocol   string             `json:"protocol"`
	TargetHost string             `json:"target_host"`
	TargetPort int                `json:"target_port"`
	Enabled    bool               `json:"enabled"`
	Paused     bool               `json:"paused"`
	Sessions   []NekoproxySession `json:"sessions"`
}

type NekoproxyChain struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type NekoproxyEntryGroup struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ListenHost string `json:"listen_host"`
	PortStart  int    `json:"port_start"`
	PortEnd    int    `json:"port_end"`
}

type GetNekoproxyBindingRequest struct {
	NodeID int64 `form:"node_id" validate:"gt=0"`
}

type NekoproxyNodeEntry struct {
	Binding *NekoproxyBinding `json:"binding,omitempty"`
	Address string            `json:"address"`
	Port    uint16            `json:"port"`
}

type NekoproxyBinding struct {
	SyncOnSave bool   `json:"sync_on_save"`
	NodeID     int64  `json:"node_id,string"`
	APIURL     string `json:"api_url"`
	ForwardID  string `json:"forward_id"`
	SessionID  string `json:"session_id"`
}

type NekoproxyState struct {
	Error    string                `json:"error,omitempty"`
	APIURL   string                `json:"api_url"`
	Forwards []NekoproxyForward    `json:"forwards"`
	Chains   []NekoproxyChain      `json:"chains"`
	Groups   []NekoproxyEntryGroup `json:"groups"`
	Bindings []NekoproxyBinding    `json:"bindings"`
}

type BindNekoproxyRequest struct {
	NodeID    int64  `json:"node_id,string" validate:"gt=0"`
	ForwardID string `json:"forward_id" validate:"required,max=128"`
	SessionID string `json:"session_id" validate:"required,max=128"`
}

type UnbindNekoproxyRequest struct {
	NodeID int64 `json:"node_id,string" validate:"gt=0"`
}

type NekoproxyBindingUpdate struct {
	Action    string `json:"action" validate:"oneof=bind unbind sync"`
	ForwardID string `json:"forward_id,omitempty" validate:"max=128"`
	SessionID string `json:"session_id,omitempty" validate:"max=128"`
}

type SyncNekoproxyRequest struct {
	NodeID int64 `json:"node_id,string,omitempty" validate:"gte=0"`
}

type NekoproxySyncError struct {
	NodeID  int64  `json:"node_id,string"`
	Message string `json:"message"`
}

type NekoproxySyncResponse struct {
	Updated int                  `json:"updated"`
	Errors  []NekoproxySyncError `json:"errors"`
}

type CreateNekoproxyForward struct {
	ChainID          string `json:"chain_id" validate:"required,max=128"`
	EntryGroupID     string `json:"entry_group_id" validate:"required,max=128"`
	Protocol         string `json:"protocol" validate:"oneof=tcp udp tcp_udp"`
	TargetHost       string `json:"target_host" validate:"required,max=255"`
	TargetPort       int    `json:"target_port" validate:"gte=1,lte=65535"`
	TargetPortManual bool   `json:"target_port_manual,omitempty"`
	RequestedPort    int    `json:"requested_port" validate:"gte=0,lte=65535"`
}

type NekoproxyNodeOptions struct {
	ForwardID string                  `json:"forward_id,omitempty" validate:"max=128"`
	SessionID string                  `json:"session_id,omitempty" validate:"max=128"`
	Create    *CreateNekoproxyForward `json:"create,omitempty"`
}
