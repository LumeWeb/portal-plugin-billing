package api

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/samber/lo"
	"go.lumeweb.com/httputil"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal/api/dto"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal-plugin-billing/pkg/ledger"
	"go.lumeweb.com/queryutil"
	queryutilHttp "go.lumeweb.com/queryutil/http"
	"go.uber.org/zap"
)

// handleGetBalance returns the authenticated user's current credit balance
// handlePauseOperation executes the pause operation.
// This endpoint is called after UI discovers it via POST /management.
// For API mode gateways, it executes directly. For portal mode, it returns redirect URL.
func (e *APIExtension) handlePauseOperation(c echo.Context) error {
	ctx := httputil.Context(c)
	userID, ok := e.getUser(ctx)
	if !ok {
		return ctx.Error(NewError(ErrKeyUnauthorized, fmt.Errorf("failed to get user ID")), http.StatusUnauthorized)
	}

	// Get active subscription to determine gateway
	sub, err := e.billingService.GetActiveSubscription(c.Request().Context(), userID)
	if err != nil {
		e.Logger().Error("failed to check subscription status",
			zap.Uint("user_id", userID),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeySubscriptionCheckFailed, fmt.Errorf("failed to check subscription status")), http.StatusInternalServerError)
	}

	if sub == nil {
		return ctx.Error(NewError(ErrKeyNoActiveSubscription, fmt.Errorf("no active subscription found")), http.StatusNotFound)
	}

	// Get the gateway for this subscription
	gateway, err := e.billingService.GetGateway(c.Request().Context(), sub.GatewayType)
	if err != nil {
		e.Logger().Error("failed to get payment gateway",
			zap.Uint("user_id", userID),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("failed to get payment gateway")), http.StatusInternalServerError)
	}

	// Check if gateway implements SubscriptionManager (for portal mode)
	manager, ok := gateway.(pluginCore.SubscriptionManager)
	if !ok {
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("gateway does not support subscription management")), http.StatusInternalServerError)
	}

	// Get management capabilities to determine mode
	capabilities, err := manager.GetManagementInfo(c.Request().Context(), userID)
	if err != nil {
		e.Logger().Error("failed to get management capabilities",
			zap.Uint("user_id", userID),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementCapabilitiesFailed, fmt.Errorf("failed to get management capabilities: %w", err)), http.StatusInternalServerError)
	}

	// API mode: execute directly
	if capabilities.ManagementMode == pluginCore.ModeAPI {
		// Verify operation is supported (defense in depth)
		supported, exists := capabilities.Operations[pluginCore.OperationPause]
		if !exists || !supported {
			return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("pause is not supported by this gateway")), http.StatusBadRequest)
		}

		executor, ok := gateway.(pluginCore.PauseResumeExecutor)
		if !ok {
			return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("gateway does not support pause/resume execution")), http.StatusInternalServerError)
		}

		if err := executor.ExecutePause(c.Request().Context(), userID); err != nil {
			e.Logger().Error("failed to execute pause",
				zap.Uint("user_id", userID),
				zap.String("gateway_type", sub.GatewayType),
				zap.Error(err))
			return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to pause subscription: %w", err)), http.StatusInternalServerError)
		}

		result := &pluginCore.ManagementResult{
			Action: pluginCore.ActionShowUI,
			Status: "paused",
		}
		response := dto.ManagementResultResponse{}
		if err := response.FromModel(result); err != nil {
			return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to build response: %w", err)), http.StatusInternalServerError)
		}
		return httputil.EncodeResponse(ctx, result, &response)
	}

	// Portal mode: return redirect URL
	result, err := manager.GetManagementURL(c.Request().Context(), userID, pluginCore.OperationPause)
	if err != nil {
		e.Logger().Error("failed to get pause portal URL",
			zap.Uint("user_id", userID),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to get pause URL: %w", err)), http.StatusInternalServerError)
	}

	response := dto.ManagementResultResponse{}
	if err := response.FromModel(result); err != nil {
		e.Logger().Error("failed to build pause response",
			zap.Uint("user_id", userID),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to build response: %w", err)), http.StatusInternalServerError)
	}

	return httputil.EncodeResponse(ctx, result, &response)
}

// handleResumeOperation executes the resume operation.
// This endpoint is called after UI discovers it via POST /management.
// For API mode gateways, it executes directly. For portal mode, it returns redirect URL.
func (e *APIExtension) handleResumeOperation(c echo.Context) error {
	ctx := httputil.Context(c)
	userID, ok := e.getUser(ctx)
	if !ok {
		return ctx.Error(NewError(ErrKeyUnauthorized, fmt.Errorf("failed to get user ID")), http.StatusUnauthorized)
	}

	// Get subscription to determine gateway (check active first, then paused)
	reqCtx := c.Request().Context()
	sub, err := e.billingService.GetActiveSubscription(reqCtx, userID)
	if err != nil {
		e.Logger().Error("failed to check subscription status",
			zap.Uint("user_id", userID),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeySubscriptionCheckFailed, fmt.Errorf("failed to check subscription status")), http.StatusInternalServerError)
	}

	// If no active subscription, check for paused subscription
	if sub == nil {
		sub, err = e.billingService.GetPausedSubscription(reqCtx, userID)
		if err != nil {
			e.Logger().Error("failed to check paused subscription status",
				zap.Uint("user_id", userID),
				zap.Error(err))
			return ctx.Error(NewError(ErrKeySubscriptionCheckFailed, fmt.Errorf("failed to check subscription status")), http.StatusInternalServerError)
		}
	}

	if sub == nil {
		return ctx.Error(NewError(ErrKeyNoActiveSubscription, fmt.Errorf("no paused subscription found")), http.StatusNotFound)
	}

	// Get the gateway for this subscription
	gateway, err := e.billingService.GetGateway(c.Request().Context(), sub.GatewayType)
	if err != nil {
		e.Logger().Error("failed to get payment gateway",
			zap.Uint("user_id", userID),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("failed to get payment gateway")), http.StatusInternalServerError)
	}

	// Check if gateway implements SubscriptionManager (for portal mode)
	manager, ok := gateway.(pluginCore.SubscriptionManager)
	if !ok {
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("gateway does not support subscription management")), http.StatusInternalServerError)
	}

	// Get management capabilities to determine mode
	capabilities, err := manager.GetManagementInfo(c.Request().Context(), userID)
	if err != nil {
		e.Logger().Error("failed to get management capabilities",
			zap.Uint("user_id", userID),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementCapabilitiesFailed, fmt.Errorf("failed to get management capabilities: %w", err)), http.StatusInternalServerError)
	}

	// API mode: execute directly
	if capabilities.ManagementMode == pluginCore.ModeAPI {
		// Verify operation is supported (defense in depth)
		supported, exists := capabilities.Operations[pluginCore.OperationResume]
		if !exists || !supported {
			return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("resume is not supported by this gateway")), http.StatusBadRequest)
		}

		executor, ok := gateway.(pluginCore.PauseResumeExecutor)
		if !ok {
			return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("gateway does not support pause/resume execution")), http.StatusInternalServerError)
		}

		if err := executor.ExecuteResume(c.Request().Context(), userID); err != nil {
			e.Logger().Error("failed to execute resume",
				zap.Uint("user_id", userID),
				zap.String("gateway_type", sub.GatewayType),
				zap.Error(err))
			return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to resume subscription: %w", err)), http.StatusInternalServerError)
		}

		result := &pluginCore.ManagementResult{
			Action: pluginCore.ActionShowUI,
			Status: "resumed",
		}
		response := dto.ManagementResultResponse{}
		if err := response.FromModel(result); err != nil {
			return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to build response: %w", err)), http.StatusInternalServerError)
		}
		return httputil.EncodeResponse(ctx, result, &response)
	}

	// Portal mode: return redirect URL
	result, err := manager.GetManagementURL(c.Request().Context(), userID, pluginCore.OperationResume)
	if err != nil {
		e.Logger().Error("failed to get resume portal URL",
			zap.Uint("user_id", userID),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to get resume URL: %w", err)), http.StatusInternalServerError)
	}

	response := dto.ManagementResultResponse{}
	if err := response.FromModel(result); err != nil {
		e.Logger().Error("failed to build resume response",
			zap.Uint("user_id", userID),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to build response: %w", err)), http.StatusInternalServerError)
	}

	return httputil.EncodeResponse(ctx, result, &response)
}

func (e *APIExtension) handleGetBalance(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()
	userID, ok := e.getUser(ctx)
	if !ok {
		return ctx.Error(NewError(ErrKeyUnauthorized, fmt.Errorf("failed to get user ID")), http.StatusUnauthorized)
	}

	balance, err := e.creditService.GetUserBalance(reqCtx, uint64(userID))
	if err != nil {
		e.Logger().Error("failed to get user balance",
			zap.Uint("user_id", userID),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyCreditNotFound, fmt.Errorf("failed to get user balance: %w", err)), http.StatusInternalServerError)
	}

	// Create balance view model for proper DTO conversion
	balanceView := &models.CreditsBalanceView{
		UserID:  uint64(userID),
		Balance: balance,
	}

	var resp dto.BalanceResponse
	return httputil.EncodeResponse(ctx, balanceView, &resp)
}

// handleListUserCredits returns the authenticated user's credit history
func (e *APIExtension) handleListUserCredits(c echo.Context) error {
	ctx := httputil.Context(c)
	userID, ok := e.getUser(ctx)
	if !ok {
		return ctx.Error(NewError(ErrKeyUnauthorized, fmt.Errorf("failed to get user ID")), http.StatusUnauthorized)
	}

	return queryutilHttp.ProcessListRequest(
		c.Response(),
		c.Request(),
		"credits",
		func(filters []queryutil.CrudFilter, sorts []queryutil.Sort, pagination queryutil.Pagination) ([]*models.CreditModel, int64, error) {
			// Always filter by the authenticated user's ID
			userFilter := queryutil.FieldEqual("user_id", userID)
			filters = append([]queryutil.CrudFilter{userFilter}, filters...)

			// Get credits from service
			credits, total, err := e.creditService.ListCredits(ctx.Context.Request().Context(), filters, sorts, pagination)
			if err != nil {
				e.Logger().Error("failed to list credits",
					zap.Uint("user_id", userID),
					zap.Error(err))
				return nil, 0, err
			}

			// Convert ledger.Credit to CreditModel for response
			creditModels := lo.Map(credits, func(credit ledger.Credit, _ int) *models.CreditModel {
				return convertCreditToModel(&credit)
			})

			return creditModels, total, nil
		},
		func(credit *models.CreditModel) dto.UserCreditItem {
			var resp dto.UserCreditItem
			resp.FromModel(credit)
			return resp
		},
	)
}
