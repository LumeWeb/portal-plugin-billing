package service

import (
	"context"
	"fmt"
	"github.com/go-openapi/strfmt"
	"github.com/killbill/kbcli/v3/kbclient/account"
	"github.com/killbill/kbcli/v3/kbclient/tag_definition"
	"github.com/killbill/kbcli/v3/kbmodel"
	"go.uber.org/zap"
	"strconv"
	"time"
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
		b.logger.Debug("enabling tag", zap.String("tag", tagName), zap.String("account", string(accountID)))
		_, err = b.api.Account.CreateAccountTags(ctx, &account.CreateAccountTagsParams{
			AccountID: accountID,
			Body:      []strfmt.UUID{tagDefId},
		})
		return err
	}

	if !enabled && hasTag {
		b.logger.Debug("disabling tag", zap.String("tag", tagName), zap.String("account", string(accountID)))
		_, err = b.api.Account.DeleteAccountTags(ctx, &account.DeleteAccountTagsParams{
			AccountID: accountID,
			TagDef:    []strfmt.UUID{tagDefId},
		})
		return err
	}

	return nil
}

func (b *BillingServiceDefault) verifyTagState(ctx context.Context, accountID strfmt.UUID, tagName string, desiredState bool) (bool, error) {
	// Get current account tags
	tags, err := b.api.Account.GetAccountTags(ctx, &account.GetAccountTagsParams{
		AccountID: accountID,
	})
	if err != nil {
		return false, fmt.Errorf("failed to verify tag state: %w", err)
	}

	// Check if tag exists
	var hasTag bool
	for _, tag := range tags.Payload {
		if tag.TagDefinitionName == tagName {
			hasTag = true
			break
		}
	}

	// Return whether current state matches desired state
	return hasTag == desiredState, nil
}

// ensureControlTag sets a control tag and verifies the state is correct, retrying until successful
func (b *BillingServiceDefault) ensureControlTag(ctx context.Context, accountID strfmt.UUID, tagName string, enabled bool) error {
	// First attempt to set the tag
	if err := b.setControlTag(ctx, accountID, tagName, enabled); err != nil {
		return fmt.Errorf("initial tag set failed: %w", err)
	}

	// Keep trying until the state is correct
	for {
		// Check if the state is correct
		matches, err := b.verifyTagState(ctx, accountID, tagName, enabled)
		if err != nil {
			return fmt.Errorf("failed to verify tag state: %w", err)
		}

		if matches {
			// State is correct, we're done
			return nil
		}

		b.logger.Warn("tag state verification failed, retrying",
			zap.String("tag", tagName),
			zap.String("account", string(accountID)))

		// Wait a second before retry to avoid hammering the API
		time.Sleep(time.Second)

		// Retry setting the tag
		if err := b.setControlTag(ctx, accountID, tagName, enabled); err != nil {
			return fmt.Errorf("failed to set tag on retry: %w", err)
		}
	}
}

func (b *BillingServiceDefault) ensureDisableAutoInvoicing(ctx context.Context, accountID strfmt.UUID, disable bool) error {
	b.logger.Debug("ensuring auto-invoicing is disabled",
		zap.String("account", string(accountID)))

	if err := b.ensureControlTag(ctx, accountID, TagAutoInvoicingOff, disable); err != nil {
		return fmt.Errorf("failed to ensure auto-invoicing is disabled: %w", err)
	}
	return nil
}

func (b *BillingServiceDefault) ensureDisableOverdueEnforcement(ctx context.Context, accountID strfmt.UUID, disable bool) error {
	b.logger.Debug("ensuring overdue enforcement is disabled",
		zap.String("account", string(accountID)))

	if err := b.ensureControlTag(ctx, accountID, TagOverdueEnforcementOff, disable); err != nil {
		return fmt.Errorf("failed to ensure overdue enforcement is disabled: %w", err)
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

func (b *BillingServiceDefault) ensureControlTags(ctx context.Context, accountID strfmt.UUID, enabled bool, tags ...string) error {

	for {
		// First set all tags to desired state
		for _, tag := range tags {
			if err := b.setControlTag(ctx, accountID, tag, enabled); err != nil {
				return fmt.Errorf("failed to set tag %s: %w", tag, err)
			}
		}

		// Verify all tags
		tagStates, err := b.verifyControlTags(ctx, accountID)
		if err != nil {
			return fmt.Errorf("failed to verify tags: %w", err)
		}

		// Check if all tags are in desired state
		allCorrect := true
		for _, tag := range tags {
			if tagStates[tag] != enabled {
				allCorrect = false
				b.logger.Warn("tag state mismatch",
					zap.String("account", string(accountID)),
					zap.String("tag", tag),
					zap.Bool("current", tagStates[tag]),
					zap.Bool("desired", enabled))
				break
			}
		}

		if allCorrect {
			b.logger.Info("all tags verified",
				zap.String("account", string(accountID)),
				zap.Strings("tags", tags),
				zap.Bool("enabled", enabled))
			return nil
		}

		// Wait before retry
		time.Sleep(time.Second)
	}

}

func (b *BillingServiceDefault) verifyControlTags(ctx context.Context, accountID strfmt.UUID) (map[string]bool, error) {
	tags, err := b.api.Account.GetAccountTags(ctx, &account.GetAccountTagsParams{
		AccountID: accountID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get account tags: %w", err)
	}

	// Create map of tag states
	tagStates := make(map[string]bool)
	for _, tag := range tags.Payload {
		tagStates[tag.TagDefinitionName] = true
	}

	return tagStates, nil
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
