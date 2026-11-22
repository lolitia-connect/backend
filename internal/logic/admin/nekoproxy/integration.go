package nekoproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/perfect-panel/server/ent"
	entbinding "github.com/perfect-panel/server/ent/nekoproxybinding"
	entnode "github.com/perfect-panel/server/ent/node"
	"github.com/perfect-panel/server/ent/predicate"
	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	remote "github.com/perfect-panel/server/pkg/nekoproxy"
	"github.com/perfect-panel/server/pkg/xerr"
	"go.uber.org/zap"
)

const configKey = "NekoproxyConnection"

type connection struct {
	APIURL string `json:"api_url"`
	Token  string `json:"api_token"`
}

type Logic struct {
	ctx context.Context
	svc *svc.ServiceContext
}

func NewLogic(ctx context.Context, service *svc.ServiceContext) *Logic {
	return &Logic{ctx: ctx, svc: service}
}
func inputError(message string) error { return xerr.NewErrCodeMsg(xerr.InvalidParams, message) }
func remoteError(err error) error     { return xerr.NewErrMsg(err.Error()) }

func loadConnection(ctx context.Context, store repository.Store) (connection, error) {
	row, err := store.System().FindByCategoryKey(ctx, "nekoproxy", configKey)
	if ent.IsNotFound(err) {
		return connection{}, nil
	}
	if err != nil {
		return connection{}, err
	}
	var config connection
	if err := json.Unmarshal([]byte(row.Value), &config); err != nil {
		return connection{}, err
	}
	return config, nil
}

func (l *Logic) Config() (*types.NekoproxyConfig, error) {
	config, err := loadConnection(l.ctx, l.svc.Store)
	if err != nil {
		return nil, err
	}
	return &types.NekoproxyConfig{APIURL: config.APIURL, HasAPIToken: config.Token != ""}, nil
}

func (l *Logic) UpdateConfig(req *types.UpdateNekoproxyConfigRequest) error {
	base, err := remote.NormalizeURL(req.APIURL)
	if err != nil {
		return inputError(err.Error())
	}
	old, err := loadConnection(l.ctx, l.svc.Store)
	if err != nil {
		return err
	}
	token := strings.TrimSpace(req.APIToken)
	if token == "" {
		if base != old.APIURL {
			return inputError("enter an API token when changing the API address")
		}
		token = old.Token
	}
	client, err := remote.New(base, token)
	if err != nil {
		return inputError(err.Error())
	}
	if err := client.Check(l.ctx); err != nil {
		return remoteError(err)
	}
	payload, err := json.Marshal(connection{APIURL: base, Token: token})
	if err != nil {
		return err
	}
	return l.svc.Store.System().UpsertByCategoryKey(l.ctx, "nekoproxy", configKey, string(payload), "string", "Nekoproxy API connection")
}

func (l *Logic) client() (connection, *remote.Client, error) {
	config, err := loadConnection(l.ctx, l.svc.Store)
	if err != nil {
		return config, nil, err
	}
	if config.APIURL == "" || config.Token == "" {
		return config, nil, inputError("configure the Nekoproxy API address and token first")
	}
	client, err := remote.New(config.APIURL, config.Token)
	if err != nil {
		return config, nil, inputError(err.Error())
	}
	return config, client, nil
}

func bindingInfo(b *ent.NekoproxyBinding) *types.NekoproxyBinding {
	if b == nil {
		return nil
	}
	return &types.NekoproxyBinding{NodeID: b.NodeID, APIURL: b.APIURL, ForwardID: b.ForwardID, SessionID: b.SessionID, SyncOnSave: b.SyncOnSave}
}

func (l *Logic) NodeEntry(nodeID int64) (*types.NekoproxyNodeEntry, error) {
	n, err := l.svc.Store.Ent().Node.Query().Where(entnode.ID(nodeID)).WithNekoproxyBinding().Only(l.ctx)
	if err != nil {
		return nil, err
	}
	return &types.NekoproxyNodeEntry{Binding: bindingInfo(n.Edges.NekoproxyBinding), Address: n.Address, Port: n.Port}, nil
}

func (l *Logic) State() (*types.NekoproxyState, error) {
	config, client, err := l.client()
	if err != nil {
		return nil, err
	}
	bindings, err := l.svc.Store.Ent().NekoproxyBinding.Query().Order(entbinding.ByNodeID()).All(l.ctx)
	if err != nil {
		return nil, err
	}
	state := &types.NekoproxyState{APIURL: config.APIURL, Forwards: []types.NekoproxyForward{}, Chains: []types.NekoproxyChain{}, Groups: []types.NekoproxyEntryGroup{}, Bindings: []types.NekoproxyBinding{}}
	for _, binding := range bindings {
		state.Bindings = append(state.Bindings, *bindingInfo(binding))
	}
	services, err := client.Services(l.ctx)
	if err != nil {
		state.Error = err.Error()
		return state, nil
	}
	sessions, err := client.Sessions(l.ctx)
	if err != nil {
		state.Error = err.Error()
		return state, nil
	}
	chains, err := client.Chains(l.ctx)
	if err != nil {
		state.Error = err.Error()
		return state, nil
	}
	groups, err := client.Groups(l.ctx)
	if err != nil {
		state.Error = err.Error()
		return state, nil
	}
	byService := make(map[string][]types.NekoproxySession)
	for _, session := range sessions {
		host, port, _ := session.Endpoint()
		byService[session.ServiceID] = append(byService[session.ServiceID], types.NekoproxySession{ID: session.SessionID, Address: host, Port: int(port), Status: session.Status, GroupID: session.EntryGroupID})
	}
	for _, service := range services {
		entries := byService[service.ID]
		if entries == nil {
			entries = []types.NekoproxySession{}
		}
		state.Forwards = append(state.Forwards, types.NekoproxyForward{ID: service.ID, Name: service.Remark, ChainID: service.ChainID, ChainName: service.ChainName, Protocol: service.Protocol, TargetHost: service.TargetHost, TargetPort: service.TargetPort, Enabled: service.Enabled && !service.ChainDisabled, Paused: service.Paused, Sessions: entries})
	}
	for _, chain := range chains {
		state.Chains = append(state.Chains, types.NekoproxyChain{ID: chain.ID, Name: chain.Name})
	}
	for _, group := range groups {
		state.Groups = append(state.Groups, types.NekoproxyEntryGroup{ID: group.ID, Name: group.Name, ListenHost: group.ListenHost, PortStart: group.PortStart, PortEnd: group.PortEnd})
	}
	return state, nil
}

type forwardEntry struct {
	*remote.Session
	service *remote.Service
}

func selectSession(ctx context.Context, client *remote.Client, forwardID, sessionID string) (*forwardEntry, error) {
	service, err := client.Service(ctx, forwardID)
	if err != nil {
		return nil, err
	}
	if !service.Enabled || service.Paused || service.ChainDisabled {
		return nil, errors.New("the selected forward or its chain is disabled or paused")
	}
	sessions, err := client.Sessions(ctx)
	if err != nil {
		return nil, err
	}
	for _, session := range sessions {
		if session.ServiceID == forwardID && session.SessionID == sessionID {
			if _, _, err := session.Endpoint(); err != nil {
				return nil, err
			}
			return &forwardEntry{Session: &session, service: service}, nil
		}
	}
	return nil, errors.New("the selected forwarding session no longer exists; refresh and select an active session")
}

func saveBinding(ctx context.Context, client *ent.Client, nodeID int64, base, forwardID, sessionID string, managed *uint16) error {
	binding, err := client.NekoproxyBinding.Query().Where(entbinding.NodeID(nodeID)).Only(ctx)
	if ent.IsNotFound(err) {
		return client.NekoproxyBinding.Create().SetNodeID(nodeID).SetAPIURL(base).SetForwardID(forwardID).SetSessionID(sessionID).SetNillableManagedTargetPort(managed).Exec(ctx)
	}
	if err != nil {
		return err
	}
	update := client.NekoproxyBinding.UpdateOneID(binding.ID).SetAPIURL(base).SetForwardID(forwardID).SetSessionID(sessionID)
	if binding.APIURL != base || binding.ForwardID != forwardID {
		update.ClearManagedTargetPort()
	}
	return update.Exec(ctx)
}

func (l *Logic) Bind(req *types.BindNekoproxyRequest) error {
	config, client, err := l.client()
	if err != nil {
		return err
	}
	session, err := selectSession(l.ctx, client, req.ForwardID, req.SessionID)
	if err != nil {
		return remoteError(err)
	}
	host, port, err := session.Endpoint()
	if err != nil {
		return inputError(err.Error())
	}
	var serverID int64
	err = l.svc.Store.InTx(l.ctx, func(store repository.Store) error {
		n, err := store.Ent().Node.Get(l.ctx, req.NodeID)
		if err != nil {
			return err
		}
		serverID = n.ServerID
		if err := saveBinding(l.ctx, store.Ent(), n.ID, config.APIURL, req.ForwardID, req.SessionID, nil); err != nil {
			return err
		}
		return store.Ent().Node.UpdateOneID(n.ID).SetAddress(host).SetPort(port).Exec(l.ctx)
	})
	if ent.IsConstraintError(err) {
		return inputError("this Nekoproxy forward is already bound to another node")
	}
	if ent.IsNotFound(err) {
		return inputError("the selected node no longer exists")
	}
	if err == nil {
		l.invalidate(serverID)
	}
	return err
}

func (l *Logic) Unbind(req *types.UnbindNekoproxyRequest) error {
	_, err := l.svc.Store.Ent().NekoproxyBinding.Delete().Where(entbinding.NodeID(req.NodeID)).Exec(l.ctx)
	return err
}

func (l *Logic) Sync(req *types.SyncNekoproxyRequest) (*types.NekoproxySyncResponse, error) {
	config, client, err := l.client()
	if err != nil {
		return nil, err
	}
	query := l.svc.Store.Ent().NekoproxyBinding.Query().Where(entbinding.APIURL(config.APIURL))
	if req.NodeID != 0 {
		query.Where(entbinding.NodeID(req.NodeID))
	}
	bindings, err := query.All(l.ctx)
	if err != nil {
		return nil, err
	}
	services, err := client.Services(l.ctx)
	if err != nil {
		return nil, remoteError(err)
	}
	sessions, err := client.Sessions(l.ctx)
	if err != nil {
		return nil, remoteError(err)
	}
	usable := make(map[string]bool)
	for _, service := range services {
		usable[service.ID] = service.Enabled && !service.Paused && !service.ChainDisabled
	}
	entries := make(map[string]remote.Session)
	for _, session := range sessions {
		entries[session.SessionID] = session
	}
	result := &types.NekoproxySyncResponse{Errors: []types.NekoproxySyncError{}}
	for _, binding := range bindings {
		session, exists := entries[binding.SessionID]
		host, port, failure := session.Endpoint()
		if !exists || session.ServiceID != binding.ForwardID || !usable[binding.ForwardID] {
			failure = errors.New("forward or session is missing, disabled or paused; rebind an active session")
		}
		var serverID int64
		if failure == nil {
			failure = l.svc.Store.InTx(l.ctx, func(store repository.Store) error {
				n, err := store.Ent().Node.Get(l.ctx, binding.NodeID)
				if err != nil {
					return err
				}
				serverID = n.ServerID
				count, err := store.Ent().Node.Update().Where(entnode.ID(n.ID), entnode.HasNekoproxyBindingWith(entbinding.ID(binding.ID), entbinding.APIURL(config.APIURL), entbinding.ForwardID(binding.ForwardID), entbinding.SessionID(binding.SessionID))).SetAddress(host).SetPort(port).Save(l.ctx)
				if err != nil {
					return err
				}
				if count == 0 {
					return errors.New("binding changed during synchronization; refresh and retry")
				}
				return nil
			})
		}
		if failure != nil {
			result.Errors = append(result.Errors, types.NekoproxySyncError{NodeID: binding.NodeID, Message: failure.Error()})
			continue
		}
		result.Updated++
		l.invalidate(serverID)
	}
	return result, nil
}

func bindingSnapshot(binding *ent.NekoproxyBinding) []predicate.NekoproxyBinding {
	match := []predicate.NekoproxyBinding{
		entbinding.ID(binding.ID), entbinding.APIURL(binding.APIURL),
		entbinding.ForwardID(binding.ForwardID), entbinding.SessionID(binding.SessionID),
		entbinding.SyncOnSave(binding.SyncOnSave),
	}
	if binding.ManagedTargetPort == nil {
		return append(match, entbinding.ManagedTargetPortIsNil())
	}
	return append(match, entbinding.ManagedTargetPort(*binding.ManagedTargetPort))
}

func (l *Logic) UpdateNode(data *node.Node, preference *bool, change *types.NekoproxyBindingUpdate) error {
	action := ""
	if change != nil {
		if err := l.svc.Validate(change); err != nil {
			return inputError(err.Error())
		}
		action = change.Action
		if action == "bind" && (change.ForwardID == "" || change.SessionID == "") {
			return inputError("select an active forwarding session")
		}
	}
	binding, err := l.svc.Store.Ent().NekoproxyBinding.Query().Where(entbinding.NodeID(data.Id)).Only(l.ctx)
	if err != nil && !ent.IsNotFound(err) {
		return err
	}
	if binding == nil && action != "bind" {
		if preference != nil || action != "" {
			return inputError("this node is no longer bound; reopen its settings")
		}
		return l.svc.Store.Node().UpdateNode(l.ctx, data)
	}
	if action == "unbind" {
		return l.svc.Store.InTx(l.ctx, func(store repository.Store) error {
			count, err := store.Ent().NekoproxyBinding.Delete().Where(bindingSnapshot(binding)...).Exec(l.ctx)
			if err != nil {
				return err
			}
			if count == 0 {
				return inputError("binding changed while saving; reopen the node and try again")
			}
			return store.Node().UpdateNode(l.ctx, data)
		})
	}

	syncOnSave := true
	var apiURL, forwardID, sessionID string
	var managedTargetPort *uint16
	if binding != nil {
		apiURL, forwardID, sessionID = binding.APIURL, binding.ForwardID, binding.SessionID
		syncOnSave, managedTargetPort = binding.SyncOnSave, binding.ManagedTargetPort
	}
	if preference != nil {
		syncOnSave = *preference
	}
	var client *remote.Client
	var entry *forwardEntry
	if syncOnSave || action != "" {
		var config connection
		config, client, err = l.client()
		if err != nil {
			return err
		}
		if action == "bind" {
			if apiURL != config.APIURL || forwardID != change.ForwardID {
				managedTargetPort = nil
			}
			apiURL, forwardID, sessionID = config.APIURL, change.ForwardID, change.SessionID
		} else if apiURL != config.APIURL {
			return inputError("this node is bound to another Nekoproxy API address")
		}
		entry, err = selectSession(l.ctx, client, forwardID, sessionID)
		if err != nil {
			return remoteError(err)
		}
		data.Address, data.Port, err = entry.Endpoint()
		if err != nil {
			return remoteError(err)
		}
	}
	var previousPort, updatedPort uint16
	err = l.svc.Store.InTx(l.ctx, func(store repository.Store) error {
		bindings := store.Ent().NekoproxyBinding
		if binding == nil {
			if err := bindings.Create().SetNodeID(data.Id).SetAPIURL(apiURL).SetForwardID(forwardID).
				SetSessionID(sessionID).SetSyncOnSave(syncOnSave).Exec(l.ctx); err != nil {
				if ent.IsConstraintError(err) {
					return inputError("this Nekoproxy forward or node is already bound")
				}
				return err
			}
		} else {
			update := bindings.UpdateOneID(binding.ID).Where(bindingSnapshot(binding)...).SetSyncOnSave(syncOnSave)
			if action == "bind" {
				update.SetAPIURL(apiURL).SetForwardID(forwardID).SetSessionID(sessionID)
				if managedTargetPort == nil {
					update.ClearManagedTargetPort()
				}
			}
			if err := update.Exec(l.ctx); err != nil {
				if ent.IsNotFound(err) {
					return inputError("binding changed while saving; reopen the node and try again")
				}
				if action == "bind" && ent.IsConstraintError(err) {
					return inputError("this Nekoproxy forward is already bound to another node")
				}
				return err
			}
		}
		old, err := store.Node().FindOneNode(l.ctx, data.Id)
		if err != nil {
			return err
		}
		if err := store.Node().UpdateNode(l.ctx, data); err != nil {
			return err
		}
		if !syncOnSave || (old.ServerId == data.ServerId && old.Protocol == data.Protocol && old.ProtocolId == data.ProtocolId) {
			return nil
		}
		port, err := managedPortOnSave(l.ctx, store, old, data, managedTargetPort, entry.service.TargetPort)
		if err != nil {
			return err
		}
		if managedTargetPort == nil || *managedTargetPort != port {
			if err := bindings.Update().Where(entbinding.NodeID(data.Id)).SetManagedTargetPort(port).Exec(l.ctx); err != nil {
				return err
			}
		}
		if port == 0 || int(port) == entry.service.TargetPort {
			return nil
		}
		// Patch last, while holding the binding lock. A rejected remote write rolls back all local edits.
		previousPort = uint16(entry.service.TargetPort)
		if err := client.UpdateTargetPort(l.ctx, forwardID, previousPort, port); err != nil {
			return remoteError(fmt.Errorf("could not confirm target port update for Nekoproxy forward %s; check its target before retrying: %w", forwardID, err))
		}
		updatedPort = port
		return nil
	})
	if err != nil && updatedPort != 0 {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(l.ctx), 15*time.Second)
		defer cancel()
		if undoErr := client.UpdateTargetPort(cleanupCtx, forwardID, updatedPort, previousPort); undoErr != nil {
			return remoteError(fmt.Errorf("node save failed; verify target port of Nekoproxy forward %s: %w", forwardID, undoErr))
		}
	}
	return err
}

func (l *Logic) CreateNode(data *node.Node, options *types.NekoproxyNodeOptions) error {
	if options == nil {
		return inputError("missing forwarding options")
	}
	existing := options.ForwardID != "" && options.SessionID != ""
	if (options.Create == nil && !existing) || (options.Create != nil && (options.ForwardID != "" || options.SessionID != "")) {
		return inputError("select either an existing forwarding session or create a new forward")
	}
	config, client, err := l.client()
	if err != nil {
		return err
	}
	forwardID := options.ForwardID
	var session *remote.Session
	created := false
	if options.Create != nil {
		if err := l.svc.Validate(options.Create); err != nil {
			return inputError(err.Error())
		}
		input := options.Create
		var service *remote.Service
		service, err = client.Create(l.ctx, remote.CreateService{ChainID: input.ChainID, TargetHost: strings.TrimSpace(input.TargetHost), TargetPort: input.TargetPort, Protocol: input.Protocol, Remark: data.Name})
		if err != nil {
			return remoteError(err)
		}
		forwardID, created = service.ID, true
		session, err = client.Issue(l.ctx, forwardID, remote.IssueSession{EntryGroupID: input.EntryGroupID, RequestedPort: input.RequestedPort})
	} else {
		var entry *forwardEntry
		entry, err = selectSession(l.ctx, client, forwardID, options.SessionID)
		if err == nil {
			session = entry.Session
		}
	}
	if err == nil {
		data.Address, data.Port, err = session.Endpoint()
	}
	if err == nil {
		err = l.svc.Store.InTx(l.ctx, func(store repository.Store) error {
			if err := store.Node().InsertNode(l.ctx, data); err != nil {
				return err
			}
			var managed *uint16
			if input := options.Create; input != nil {
				port := uint16(0)
				if !input.TargetPortManual && data.ServerId != 0 && data.ProtocolId != "" {
					protocolPort, err := selectedPort(l.ctx, store, data, false)
					if err != nil {
						return err
					}
					if int(protocolPort) == input.TargetPort {
						port = protocolPort
					}
				}
				managed = &port
			}
			return saveBinding(l.ctx, store.Ent(), data.Id, config.APIURL, forwardID, session.SessionID, managed)
		})
	}
	if err != nil {
		if created {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(l.ctx), 15*time.Second)
			defer cancel()
			if cleanupErr := client.Delete(cleanupCtx, forwardID); cleanupErr != nil {
				return remoteError(fmt.Errorf("node creation failed; remove Nekoproxy forward %s manually: %s", forwardID, cleanupErr))
			}
		}
		if ent.IsConstraintError(err) {
			return inputError("this Nekoproxy forward is already bound to another node")
		}
		return remoteError(err)
	}
	l.invalidate(data.ServerId)
	return nil
}

func (l *Logic) invalidate(serverID int64) {
	if l.svc.Redis == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(l.ctx), 10*time.Second)
	defer cancel()
	if err := l.svc.Store.Node().ClearServerCache(ctx, serverID); err != nil {
		zap.S().Warnw("Nekoproxy node saved but cache invalidation failed", "server_id", serverID, "error", err)
	}
}
