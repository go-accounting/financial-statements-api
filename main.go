package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"sync"
	"time"

	apisupport "github.com/go-accounting/api-support"
	financialstatements "github.com/go-accounting/financial-statements"
	"github.com/julienschmidt/httprouter"
)

var api *apisupport.Api

var datasourceSettings map[string]interface{}

var newDataSource func(map[string]interface{}, *string, *string) (interface{}, error)

type reportgenerator struct {
	*financialstatements.ReportGenerator
	user  string
	coaid string
}

var reportgeneratorPool = sync.Pool{
	New: func() interface{} {
		r := &reportgenerator{}
		v, err := newDataSource(datasourceSettings, &r.user, &r.coaid)
		if err != nil {
			panic(err)
		}
		r.ReportGenerator = financialstatements.NewReportGenerator(v.(financialstatements.DataSource))
		return r
	},
}

type decoder func(interface{}) error

func handler(
	f func(*reportgenerator, httprouter.Params, url.Values, decoder) (interface{}, error),
) func(http.ResponseWriter, *http.Request, httprouter.Params) {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		user, err := api.UserFromRequest(r)
		if api.Check(err, w) {
			return
		}
		rg := reportgeneratorPool.Get().(*reportgenerator)
		rg.user = user
		rg.coaid = ps.ByName("coa")
		defer reportgeneratorPool.Put(rg)
		v, err := f(rg, ps, r.URL.Query(), func(v interface{}) error {
			return json.NewDecoder(r.Body).Decode(v)
		})
		if api.Check(err, w) {
			return
		}
		if v != nil {
			w.Header().Set("Content-Type", "application/json")
			api.Check(json.NewEncoder(w).Encode(v), w)
		}
	}
}

func balance(rg *reportgenerator, ps httprouter.Params, uv url.Values, d decoder) (interface{}, error) {
	var at time.Time
	if err := parse(uv.Get("at")).into(&at); err != nil {
		return nil, err
	}
	return rg.BalanceSheet(at)
}

func incomeStatement(rg *reportgenerator, ps httprouter.Params, uv url.Values, d decoder) (interface{}, error) {
	var from, to time.Time
	if err := parse(uv.Get("from"), uv.Get("to")).into(&from, &to); err != nil {
		return nil, err
	}
	return rg.IncomeStatement(from, to)
}

func journal(rg *reportgenerator, ps httprouter.Params, uv url.Values, d decoder) (interface{}, error) {
	var from, to time.Time
	if err := parse(uv.Get("from"), uv.Get("to")).into(&from, &to); err != nil {
		return nil, err
	}
	return rg.Journal(from, to)
}

func ledger(rg *reportgenerator, ps httprouter.Params, uv url.Values, d decoder) (interface{}, error) {
	var from, to time.Time
	if err := parse(uv.Get("from"), uv.Get("to")).into(&from, &to); err != nil {
		return nil, err
	}
	return rg.Ledger(ps.ByName("accountid"), from, to)
}

type parseable []string

func parse(ss ...string) parseable { return ss }

func (ss parseable) into(tt ...*time.Time) (err error) {
	for i, s := range ss {
		if *tt[i], err = time.Parse("2006-01-02", s); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("usage: %v settings", path.Base(os.Args[0]))
		return
	}
	api = apisupport.NewApi()
	var settings struct {
		FinancialStatements struct {
			PluginFile string                 `yaml:"PluginFile"`
			Settings   map[string]interface{} `yaml:",inline"`
		} `yaml:"FinancialStatements"`
		OpenId struct {
			Provider string `yaml:"Provider"`
			ClientId string `yaml:"ClientId"`
		} `yaml:"OpenId"`
	}
	err := api.UnmarshalSettings(os.Args[1], &settings)
	if err != nil {
		log.Fatal(err)
	}
	api.SetClientCredentials(settings.OpenId.Provider, settings.OpenId.ClientId)
	if api.Error() != nil {
		log.Fatal(api.Error())
	}
	symbol, err := api.LoadSymbol(settings.FinancialStatements.PluginFile, "NewDataSource")
	if err != nil {
		log.Fatal(err)
	}
	newDataSource = symbol.(func(map[string]interface{}, *string, *string) (interface{}, error))
	symbol, err = api.LoadSymbol(settings.FinancialStatements.PluginFile, "LoadSymbolFunction")
	if err == nil {
		*symbol.(*func(string, string) (interface{}, error)) = api.LoadSymbol
	}
	datasourceSettings = settings.FinancialStatements.Settings
	router := httprouter.New()
	router.GET("/charts-of-accounts/:coa/balance-sheet", handler(balance))
	router.GET("/charts-of-accounts/:coa/income-statement", handler(incomeStatement))
	router.GET("/charts-of-accounts/:coa/journal", handler(journal))
	router.GET("/charts-of-accounts/:coa/accounts/:accountid/ledger", handler(ledger))
	log.Fatal(http.ListenAndServe(":8080", router))
}
