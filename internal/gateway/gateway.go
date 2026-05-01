package gateway

import (
	"context"
	"errors"
	"fmt"
	"sync"

	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal/core"
)

// Registry maintains a collection of payment gateways
type Registry struct {
	gateways *pluginCore.OrderedMap[string, pluginCore.GatewayIdentity]
	mu       sync.RWMutex
}

// NewRegistry creates a new empty gateway registry
func NewRegistry() *Registry {
	return &Registry{
		gateways: pluginCore.NewOrderedMap[string, pluginCore.GatewayIdentity](),
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
	r.gateways = pluginCore.NewOrderedMap[string, pluginCore.GatewayIdentity]()
}

var (
	defaultRegistry = NewRegistry()
)

// Predefined errors for registry operations
var (
	ErrGatewayAlreadyRegistered = errors.New("gateway already registered")
	ErrGatewayNotFound          = errors.New("gateway not found")
)

// Register adds a payment gateway to the registry
func (r *Registry) Register(ctx context.Context, gateway pluginCore.GatewayIdentity) error {
	ctx, span := core.TraceMethod(ctx, "Registry.Register")
	defer span.End()

	r.mu.Lock()
	defer r.mu.Unlock()

	if gateway == nil {
		GatewayRegistered.WithLabelValues("", LabelStatusError).Inc()
		return fmt.Errorf("gateway cannot be nil")
	}

	id := gateway.ID(ctx)
	if id == "" {
		GatewayRegistered.WithLabelValues("", LabelStatusError).Inc()
		return fmt.Errorf("gateway ID cannot be empty")
	}
	if _, exists := r.gateways.Get(id); exists {
		GatewayRegistered.WithLabelValues(id, LabelStatusError).Inc()
		return fmt.Errorf("%w: %s", ErrGatewayAlreadyRegistered, id)
	}
	r.gateways.Set(id, gateway)
	GatewayRegistered.WithLabelValues(id, LabelStatusSuccess).Inc()
	return nil
}

// Get retrieves a payment gateway by its ID
func (r *Registry) Get(id string) (pluginCore.GatewayIdentity, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.gateways.Get(id)
}

// GetAll returns all registered payment gateways
func (r *Registry) GetAll() []pluginCore.GatewayIdentity {
	r.mu.RLock()
	defer r.mu.RUnlock()

	gateways := make([]pluginCore.GatewayIdentity, 0, r.gateways.Len())
	r.gateways.Range(func(_ string, gateway pluginCore.GatewayIdentity) bool {
		gateways = append(gateways, gateway)
		return true
	})
	return gateways
}

// GetAllGateways returns all registered payment gateways in insertion order
func (r *Registry) GetAllGateways() *pluginCore.OrderedMap[string, pluginCore.GatewayIdentity] {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.gateways
}

// ValidateWebhook validates a webhook for a specific gateway
func (r *Registry) ValidateWebhook(ctx context.Context, gatewayType string, signature string, payload []byte) error {
	ctx, span := core.TraceMethod(ctx, "Registry.ValidateWebhook")
	defer span.End()

	gw, exists := r.Get(gatewayType)
	if !exists {
		WebhookValidated.WithLabelValues(gatewayType, LabelStatusError).Inc()
		return pluginCore.ErrGatewayNotFound
	}

	webhookHandler, handlerErr := pluginCore.AsWebhookHandler(gw)
	if handlerErr != nil {
		WebhookValidated.WithLabelValues(gatewayType, LabelStatusError).Inc()
		return fmt.Errorf("gateway %s does not implement WebhookHandler", gatewayType)
	}

	validationErr := webhookHandler.ValidateWebhook(ctx, signature, payload)
	if validationErr != nil {
		WebhookValidated.WithLabelValues(gatewayType, LabelStatusError).Inc()
		return validationErr
	}
	WebhookValidated.WithLabelValues(gatewayType, LabelStatusSuccess).Inc()
	return nil
}

// GetSignatureHeader returns the signature header name for a gateway
func (r *Registry) GetSignatureHeader(ctx context.Context, gatewayType string) (string, error) {
	ctx, span := core.TraceMethod(ctx, "Registry.GetSignatureHeader")
	defer span.End()

	gw, exists := r.Get(gatewayType)
	if !exists {
		return "", pluginCore.ErrGatewayNotFound
	}

	webhookHandler, err := pluginCore.AsWebhookHandler(gw)
	if err != nil {
		return "", fmt.Errorf("gateway %s does not implement WebhookHandler", gatewayType)
	}
	return webhookHandler.SignatureHeader(ctx), nil
}

// HandleWebhook handles a webhook for a specific gateway
func (r *Registry) HandleWebhook(ctx context.Context, gatewayType string, payload []byte) error {
	ctx, span := core.TraceMethod(ctx, "Registry.HandleWebhook")
	defer span.End()

	gw, exists := r.Get(gatewayType)
	if !exists {
		WebhookHandled.WithLabelValues(gatewayType, LabelStatusError).Inc()
		return pluginCore.ErrGatewayNotFound
	}

	webhookHandler, handlerErr := pluginCore.AsWebhookHandler(gw)
	if handlerErr != nil {
		WebhookHandled.WithLabelValues(gatewayType, LabelStatusError).Inc()
		return fmt.Errorf("gateway %s does not implement WebhookHandler", gatewayType)
	}

	handleErr := webhookHandler.HandleWebhook(ctx, payload)
	if handleErr != nil {
		WebhookHandled.WithLabelValues(gatewayType, LabelStatusError).Inc()
		return handleErr
	}
	WebhookHandled.WithLabelValues(gatewayType, LabelStatusSuccess).Inc()
	return nil
}

// ExtractEventID extracts the event ID from a webhook payload for a specific gateway
func (r *Registry) ExtractEventID(ctx context.Context, gatewayType string, payload []byte) (string, error) {
	ctx, span := core.TraceMethod(ctx, "Registry.ExtractEventID")
	defer span.End()

	gw, exists := r.Get(gatewayType)
	if !exists {
		return "", pluginCore.ErrGatewayNotFound
	}

	webhookHandler, err := pluginCore.AsWebhookHandler(gw)
	if err != nil {
		return "", fmt.Errorf("gateway %s does not implement WebhookHandler", gatewayType)
	}

	return webhookHandler.ExtractEventID(ctx, payload)
}

// ExtractEventType extracts the event type from a webhook payload for a specific gateway
func (r *Registry) ExtractEventType(ctx context.Context, gatewayType string, payload []byte) (string, error) {
	ctx, span := core.TraceMethod(ctx, "Registry.ExtractEventType")
	defer span.End()

	gw, exists := r.Get(gatewayType)
	if !exists {
		return "", pluginCore.ErrGatewayNotFound
	}

	webhookHandler, err := pluginCore.AsWebhookHandler(gw)
	if err != nil {
		return "", fmt.Errorf("gateway %s does not implement WebhookHandler", gatewayType)
	}

	return webhookHandler.ExtractEventType(ctx, payload)
}
