package service

import (
	"context"
	"fmt"
	"github.com/go-openapi/strfmt"
	"github.com/killbill/kbcli/v3/kbclient/account"
	"github.com/killbill/kbcli/v3/kbclient/tag_definition"
	"github.com/killbill/kbcli/v3/kbmodel"
	"strconv"
)

const AUTO_PAY_TAG_NAME = "AUTO_PAY_OFF"

func (b *BillingServiceDefault) setAutoPay(ctx context.Context, accountID strfmt.UUID, enabled bool) error {
	tagDefId, err := b.getAutoPayTagDefId(ctx)

	tags, err := b.api.Account.GetAccountTags(ctx, &account.GetAccountTagsParams{
		AccountID: accountID,
	})
	if err != nil {
		return err
	}

	var found bool

	for _, tag := range tags.Payload {
		if tag.TagDefinitionName == AUTO_PAY_TAG_NAME {
			found = true
			break
		}
	}

	if enabled {
		if found {
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

	if !found {
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

func (b *BillingServiceDefault) getAutoPayTagDefId(ctx context.Context) (strfmt.UUID, error) {
	tagDefs, err := b.api.TagDefinition.GetTagDefinitions(ctx, &tag_definition.GetTagDefinitionsParams{})

	if err != nil {
		return "", err
	}

	for _, tagDef := range tagDefs.Payload {
		if *tagDef.Name == AUTO_PAY_TAG_NAME && tagDef.IsControlTag {
			return tagDef.ID, nil
		}
	}

	return "", fmt.Errorf("cannot find AUTO_PAY_OFF tag definition")
}

func (b *BillingServiceDefault) getAccount(ctx context.Context, userID uint) (*kbmodel.Account, error) {
	err := b.CreateCustomerById(ctx, userID)
	if err != nil {
		return nil, err
	}

	acct, err := b.api.Account.GetAccountByKey(ctx, &account.GetAccountByKeyParams{
		ExternalKey: strconv.FormatUint(uint64(userID), 10),
	})

	if err != nil {
		return nil, err
	}

	return acct.Payload, nil
}

func (b *BillingServiceDefault) getSubscription(ctx context.Context, userID uint) (*kbmodel.Subscription, error) {
	acct, err := b.getAccount(ctx, userID)
	if err != nil {
		return nil, err
	}
	
	bundles, err := b.api.Account.GetAccountBundles(ctx, &account.GetAccountBundlesParams{
		AccountID: acct.AccountID,
	})

	if err != nil {
		return nil, err
	}

	return findActiveOrPendingSubscription(bundles.Payload), nil
}
