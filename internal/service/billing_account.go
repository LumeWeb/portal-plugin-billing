package service

import (
	"context"
	"github.com/go-openapi/strfmt"
	"github.com/killbill/kbcli/v3/kbclient/account"
)

const AUTO_PAY_TAG_NAME = "AUTO_PAY_OFF"

func (b *BillingServiceDefault) setAutoPay(ctx context.Context, accountID strfmt.UUID, enabled bool) error {
	tags, err := b.api.Account.GetAccountTags(ctx, &account.GetAccountTagsParams{
		AccountID: accountID,
	})
	if err != nil {
		return err
	}

	var tagDefId strfmt.UUID

	for _, tag := range tags.Payload {
		if tag.TagDefinitionName == AUTO_PAY_TAG_NAME {
			tagDefId = tag.TagDefinitionID
			break
		}
	}

	if enabled {
		if tagDefId != "" {
			_, err = b.api.Account.DeleteAccountTags(ctx, &account.DeleteAccountTagsParams{
				AccountID: accountID,
				TagDef:    []strfmt.UUID{tagDefId},
			})

			if err != nil {
				return err
			}

			return nil
		}
	}

	if tagDefId == "" {
		_, err = b.api.Account.CreateAccountTags(ctx, &account.CreateAccountTagsParams{
			AccountID: accountID,
			Body:      []strfmt.UUID{tagDefId},
		})

		if err != nil {
			return err
		}
	}

	return nil
}
