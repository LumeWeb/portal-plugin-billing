package api

import (
	"errors"
	"github.com/Boostport/address"
	"github.com/hashicorp/go-multierror"
	"github.com/samber/lo"
	"go.lumeweb.com/httputil"
	"go.lumeweb.com/portal-plugin-billing/internal/api/messages"
	"go.lumeweb.com/portal/core"
	"go.lumeweb.com/portal/middleware"
	"net/http"
	"sort"
)

func (a API) listBillingCountries(w http.ResponseWriter, r *http.Request) {
	ctx := httputil.Context(r, w)

	countries := address.ListCountries("en")

	response := messages.CountriesResponse{
		Items: lo.Map(countries, func(item address.CountryListItem, _ int) messages.Country {
			countryData := address.GetCountry(item.Code)
			return messages.Country{
				Location: messages.Location{
					Code: item.Code,
					Name: item.Name,
				},
				SupportedFields: lo.Map(countryData.Allowed, func(field address.Field, _ int) string {
					return field.Key()
				}),
			}
		}),
	}

	ctx.Encode(response)
}

func (a API) listBillingStates(w http.ResponseWriter, r *http.Request) {
	ctx := httputil.Context(r, w)

	var country string
	if err := ctx.DecodeForm("country", &country); err != nil {
		_ = ctx.Error(err, http.StatusBadRequest)
		return
	}

	countryData := address.GetCountry(country)

	states := lo.Reduce(
		lo.Keys(countryData.AdministrativeAreas),
		func(acc []messages.Location, key string, _ int) []messages.Location {
			areas := countryData.AdministrativeAreas[key]
			for _, area := range areas {
				acc = append(acc, messages.Location{
					Code: area.ID,
					Name: area.Name,
				})
			}
			return acc
		},
		[]messages.Location{},
	)

	ctx.Encode(messages.LocationsResponse{Items: states})
}

func (a API) listBillingCities(w http.ResponseWriter, r *http.Request) {
	ctx := httputil.Context(r, w)

	var country, state string
	if err := ctx.DecodeForm("country", &country); err != nil {
		_ = ctx.Error(err, http.StatusBadRequest)
		return
	}
	if err := ctx.DecodeForm("state", &state); err != nil {
		_ = ctx.Error(err, http.StatusBadRequest)
		return
	}

	countryData := address.GetCountry(country)

	cities := lo.Reduce(
		lo.Values(countryData.AdministrativeAreas),
		func(acc []messages.Location, areas []address.AdministrativeAreaData, _ int) []messages.Location {
			matchingArea, found := lo.Find(areas, func(area address.AdministrativeAreaData) bool {
				return area.ID == state
			})

			if found {
				return append(acc, lo.Map(matchingArea.Localities, func(locality address.LocalityData, _ int) messages.Location {
					return messages.Location{
						Code: locality.ID,
						Name: locality.Name,
					}
				})...)
			}
			return acc
		},
		[]messages.Location{},
	)

	// Sort cities alphabetically by name
	sort.Slice(cities, func(i, j int) bool {
		return cities[i].Name < cities[j].Name
	})

	ctx.Encode(messages.LocationsResponse{Items: cities})
}

func (a API) updateBilling(w http.ResponseWriter, r *http.Request) {
	ctx := httputil.Context(r, w)

	user, err := middleware.GetUserFromContext(ctx)
	if err != nil {
		_ = ctx.Error(core.NewAccountError(core.ErrKeyInvalidLogin, nil), http.StatusUnauthorized)
		return
	}

	var req messages.UpdateBillingRequest
	if err := ctx.Decode(&req); err != nil {
		_ = ctx.Error(err, http.StatusBadRequest)
		return
	}

	if err := a.billingService.UpdateBillingInfo(ctx, user, &req.Billing); err != nil {
		var merr *multierror.Error
		if errors.As(err, &merr) {
			details := make(map[string]string)

			for _, subErr := range merr.Errors {
				switch {
				case errors.Is(subErr, address.ErrInvalidCountryCode):
					details["country"] = subErr.Error()
				case errors.Is(subErr, address.ErrInvalidAdministrativeArea):
					details["state"] = subErr.Error()
				case errors.Is(subErr, address.ErrInvalidLocality):
					details["city"] = subErr.Error()
				case errors.Is(subErr, address.ErrInvalidPostCode):
					details["postal_code"] = subErr.Error()
				}
			}

			ctx.Response.WriteHeader(http.StatusBadRequest)
			ctx.Encode(&messages.ErrorResponse{
				Code:    "INVALID_BILLING_INFO",
				Message: "Invalid billing information provided",
				Details: details,
			})
			return
		}
		_ = ctx.Error(err, http.StatusInternalServerError)
		return
	}

	// Return updated subscription info
	subscription, err := a.billingService.GetSubscription(ctx, user)
	if err != nil {
		_ = ctx.Error(err, http.StatusInternalServerError)
		return
	}

	ctx.Encode(messages.SubscriptionResponse{
		Subscription: subscription,
	})
}
