package gateway

import (
	"context"
	"fmt"
	"sync"

	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
)


// Registry maintains a collection of payment gateways
type Registry struct {
	gateways map[string]pluginCore.PaymentGateway
	mu       sync.RWMutex
}

// NewRegistry creates a new empty gateway registry
func NewRegistry() *Registry {
	return &Registry{
		gateways: make(map[string]pluginCore.PaymentGateway),
	}
}

// GetRegistry returns the singleton gateway registry instance
func GetRegistry() *Registry {
	return defaultRegistry
}

// Reset clears all registered gateways from the registry
// This is useful for testing purposes
func (r *Registry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gateways = make(map[string]pluginCore.PaymentGateway)
}

var (
	defaultRegistry = NewRegistry()
)

// Register adds a payment gateway to the registry
func (r *Registry) Register(gateway pluginCore.PaymentGateway) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if gateway == nil {
		return fmt.Errorf("gateway cannot be nil")
	}

	id := gateway.ID()
	if id == "" {
		return fmt.Errorf("gateway ID cannot be empty")
	}
	if _, exists := r.gateways[id]; exists {
		return fmt.Errorf("gateway %q already registered", id)
	}
	r.gateways[id] = gateway
	return nil
}

// Get retrieves a payment gateway by its ID
func (r *Registry) Get(id string) (pluginCore.PaymentGateway, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	gateway, exists := r.gateways[id]
	return gateway, exists
}

// GetAll returns all registered payment gateways
func (r *Registry) GetAll() []pluginCore.PaymentGateway {
	r.mu.RLock()
	defer r.mu.RUnlock()

	gateways := make([]pluginCore.PaymentGateway, 0, len(r.gateways))
	for _, gateway := range r.gateways {
		gateways = append(gateways, gateway)
	}
	return gateways
}


// ValidateWebhook validates a webhook for a specific gateway
func (r *Registry) ValidateWebhook(ctx context.Context, gatewayType string, signature string, payload []byte) error {
	gw, exists := r.Get(gatewayType)
	if !exists {
		return pluginCore.ErrGatewayNotFound
	}

	return gw.ValidateWebhook(ctx, signature, payload)
}

// GetSignatureHeader returns the signature header name for a gateway
func (r *Registry) GetSignatureHeader(gatewayType string) (string, error) {
	gw, exists := r.Get(gatewayType)
	if !exists {
		return "", pluginCore.ErrGatewayNotFound
	}
	return gw.SignatureHeader(), nil
}

// HandleWebhook handles a webhook for a specific gateway
func (r *Registry) HandleWebhook(ctx context.Context, gatewayType string, payload []byte) error {
	gw, exists := r.Get(gatewayType)
	if !exists {
		return pluginCore.ErrGatewayNotFound
	}

	return gw.HandleWebhook(ctx, payload)
}

// ExtractEventID extracts the event ID from a webhook payload for a specific gateway
func (r *Registry) ExtractEventID(gatewayType string, payload []byte) (string, error) {
	gw, exists := r.Get(gatewayType)
	if !exists {
		return "", pluginCore.ErrGatewayNotFound
	}

	return gw.ExtractEventID(payload)
}

// ExtractEventType extracts the event type from a webhook payload for a specific gateway
func (r *Registry) ExtractEventType(gatewayType string, payload []byte) (string, error) {
	gw, exists := r.Get(gatewayType)
	if !exists {
		return "", pluginCore.ErrGatewayNotFound
	}

	return gw.ExtractEventType(payload)
}
