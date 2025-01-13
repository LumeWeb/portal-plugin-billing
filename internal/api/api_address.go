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

	countriesResponse := lo.Map(countries, func(item address.CountryListItem, _ int) *messages.ListBillingCountriesResponseItem {
		countryData := address.GetCountry(item.Code)
		return &messages.ListBillingCountriesResponseItem{
			Code: item.Code,
			Name: item.Name,
			SupportedEntities: lo.Map(countryData.Allowed, func(entity address.Field, _ int) string {
				return entity.Key()
			}),
		}
	})

	ctx.Encode(countriesResponse)
}

func (a API) listBillingStates(w http.ResponseWriter, r *http.Request) {
	ctx := httputil.Context(r, w)

	var country string

	err := ctx.DecodeForm("country", &country)
	if err != nil {
		_ = ctx.Error(err, http.StatusBadRequest)
		return
	}

	countryData := address.GetCountry(country)

	states := lo.Reduce(
		lo.Keys(countryData.AdministrativeAreas),
		func(acc []*messages.ListBillingStatesResponseItem, key string, _ int) []*messages.ListBillingStatesResponseItem {
			areas := countryData.AdministrativeAreas[key]
			for _, area := range areas {
				acc = append(acc, &messages.ListBillingStatesResponseItem{
					Code: area.ID,
					Name: area.Name,
				})
			}
			return acc
		},
		[]*messages.ListBillingStatesResponseItem{},
	)

	ctx.Encode(states)
}

func (a API) listBillingCities(w http.ResponseWriter, r *http.Request) {
	ctx := httputil.Context(r, w)

	var country, state string

	err := ctx.DecodeForm("country", &country)
	if err != nil {
		_ = ctx.Error(err, http.StatusBadRequest)
		return
	}

	err = ctx.DecodeForm("state", &state)
	if err != nil {
		_ = ctx.Error(err, http.StatusBadRequest)
		return
	}

	countryData := address.GetCountry(country)

	cities := lo.Reduce(
		lo.Values(countryData.AdministrativeAreas),
		func(acc []*messages.ListBillingCitiesResponseItem, areas []address.AdministrativeAreaData, _ int) []*messages.ListBillingCitiesResponseItem {
			matchingArea, found := lo.Find(areas, func(area address.AdministrativeAreaData) bool {
				return area.ID == state
			})

			if found {
				return append(acc, lo.Map(matchingArea.Localities, func(locality address.LocalityData, _ int) *messages.ListBillingCitiesResponseItem {
					return &messages.ListBillingCitiesResponseItem{
						Code: locality.ID,
						Name: locality.Name,
					}
				})...)
			}

			return acc
		},
		[]*messages.ListBillingCitiesResponseItem{},
	)

	// Sort cities alphabetically by name
	sort.Slice(cities, func(i, j int) bool {
		return cities[i].Name < cities[j].Name
	})

	ctx.Encode(cities)
}

func (a API) updateBilling(w http.ResponseWriter, r *http.Request) {
	ctx := httputil.Context(r, w)

	user, err := middleware.GetUserFromContext(ctx)

	if err != nil {
		_ = ctx.Error(core.NewAccountError(core.ErrKeyInvalidLogin, nil), http.StatusUnauthorized)
		return
	}

	var billingInfo messages.BillingInfo
	if err := ctx.Decode(&billingInfo); err != nil {
		_ = ctx.Error(err, http.StatusInternalServerError)
		return
	}

	if err := a.billingService.UpdateBillingInfo(ctx, user, &billingInfo); err != nil {
		errs :=
			make([]*messages.UpdateBillingInfoResponseErrorItem, 0)
		if merr, ok := errors.Unwrap(err).(*multierror.Error); ok {
			for _, subErr := range merr.Errors {
				switch {
				case errors.Is(subErr, address.ErrInvalidCountryCode):
					errs = append(errs, &messages.UpdateBillingInfoResponseErrorItem{
						Field:   "country",
						Message: subErr.Error(),
					})
				case errors.Is(subErr, address.ErrInvalidAdministrativeArea):
					errs = append(errs, &messages.UpdateBillingInfoResponseErrorItem{
						Field:   "state",
						Message: subErr.Error(),
					})
				case errors.Is(subErr, address.ErrInvalidLocality):
					errs = append(errs, &messages.UpdateBillingInfoResponseErrorItem{
						Field:   "city",
						Message: subErr.Error(),
					})
				case errors.Is(subErr, address.ErrInvalidPostCode):
					errs = append(errs, &messages.UpdateBillingInfoResponseErrorItem{
						Field:   "zip",
						Message: subErr.Error(),
					})
				}
			}

			ctx.Response.WriteHeader(http.StatusBadRequest)
			ctx.Encode(&messages.UpdateBillingInfoResponseError{Errors: errs})
			return
		}

		_ = ctx.Error(err, http.StatusInternalServerError)
	}
	return
}
