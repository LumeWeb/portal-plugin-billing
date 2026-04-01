package api

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/samber/lo"
	"go.lumeweb.com/httputil"
	"go.lumeweb.com/portal-plugin-billing/internal/api/dto"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal-plugin-billing/pkg/ledger"
	"go.lumeweb.com/queryutil"
	queryutilHttp "go.lumeweb.com/queryutil/http"
	"go.uber.org/zap"
)

// handleGetBalance returns the authenticated user's current credit balance
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
