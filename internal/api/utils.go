package api

import (
	"encoding/json"

	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal-plugin-billing/pkg/ledger"
	"gorm.io/datatypes"
)

// convertCreditToModel converts a ledger Credit to CreditModel for DTO conversion
func convertCreditToModel(credit *ledger.Credit) *models.CreditModel {
	if credit == nil {
		return nil
	}

	var metaJSON datatypes.JSON
	if len(credit.Metadata) > 0 {
		metaBytes, err := json.Marshal(credit.Metadata)
		if err == nil {
			metaJSON = datatypes.JSON(metaBytes)
		}
	}

	return &models.CreditModel{
		ID:            credit.ID,
		UserID:        credit.UserID,
		Amount:        credit.Amount,
		Type:          credit.Type,
		Direction:     credit.Direction,
		ReferenceID:   credit.ReferenceID,
		ReferenceType: credit.ReferenceType,
		Description:   credit.Description,
		Metadata:      metaJSON,
		CreatedBy:     credit.CreatedBy,
		CreatedAt:     credit.CreatedAt,
		UpdatedAt:     credit.UpdatedAt,
		DeletedAt:     &credit.DeletedAt,
	}
}
