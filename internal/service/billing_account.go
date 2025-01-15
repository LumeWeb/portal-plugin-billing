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

const (
	TagAutoPayOff            = "AUTO_PAY_OFF"
	TagAutoInvoicingOff      = "AUTO_INVOICING_OFF"
	TagOverdueEnforcementOff = "OVERDUE_ENFORCEMENT_OFF"
)

func (b *BillingServiceDefault) setControlTag(ctx context.Context, accountID strfmt.UUID, tagName string, enabled bool) error {
	// Get tag definition ID
	tagDefs, err := b.api.TagDefinition.GetTagDefinitions(ctx, &tag_definition.GetTagDefinitionsParams{})
	if err != nil {
		return err
	}

	var tagDefId strfmt.UUID
	for _, tagDef := range tagDefs.Payload {
		if *tagDef.Name == tagName && tagDef.IsControlTag {
			tagDefId = tagDef.ID
			break
		}
	}
	if tagDefId == "" {
		return fmt.Errorf("cannot find %s tag definition", tagName)
	}

	// Check if tag exists
	tags, err := b.api.Account.GetAccountTags(ctx, &account.GetAccountTagsParams{
		AccountID: accountID,
	})
	if err != nil {
		return err
	}

	var hasTag bool
	for _, tag := range tags.Payload {
		if tag.TagDefinitionName == tagName {
			hasTag = true
			break
		}
	}

	// Add or remove tag based on desired state
	if enabled && !hasTag {
		_, err = b.api.Account.CreateAccountTags(ctx, &account.CreateAccountTagsParams{
			AccountID: accountID,
			Body:      []strfmt.UUID{tagDefId},
		})
		return err
	}

	if !enabled && hasTag {
		_, err = b.api.Account.DeleteAccountTags(ctx, &account.DeleteAccountTagsParams{
			AccountID: accountID,
			TagDef:    []strfmt.UUID{tagDefId},
		})
		return err
	}

	return nil
}

func (b *BillingServiceDefault) disableAutoPay(ctx context.Context, accountID strfmt.UUID, disable bool) error {
	return b.setControlTag(ctx, accountID, TagAutoPayOff, disable)
}

func (b *BillingServiceDefault) disableAutoInvoicing(ctx context.Context, accountID strfmt.UUID, disable bool) error {
	return b.setControlTag(ctx, accountID, TagAutoInvoicingOff, disable)
}

func (b *BillingServiceDefault) disableOverdueEnforcement(ctx context.Context, accountID strfmt.UUID, disable bool) error {
	return b.setControlTag(ctx, accountID, TagOverdueEnforcementOff, disable)
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
