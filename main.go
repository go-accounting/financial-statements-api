package main

import (
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

type reportgenerator struct {
	*financialstatements.ReportGenerator
	user  string
	coaid string
}

var reportgeneratorPool = sync.Pool{
	New: func() interface{} {
		r := &reportgenerator{}
		v, err := api.Config().Run("NewFinancialStatementsDataSource", &r.user, &r.coaid)
		if err != nil {
			panic(err)
		}
		r.ReportGenerator = financialstatements.NewReportGenerator(v.(financialstatements.DataSource))
		return r
	},
}

func handler(
	f func(*reportgenerator, httprouter.Params, url.Values, apisupport.Decoder) (interface{}, error),
) func(http.ResponseWriter, *http.Request, httprouter.Params) {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		user := api.UserFromRequest(w, r)
		if user == "" {
			return
		}
		rg := reportgeneratorPool.Get().(*reportgenerator)
		rg.user = user
		rg.coaid = ps.ByName("coa")
		defer reportgeneratorPool.Put(rg)
		api.Run(w, func() (interface{}, error) {
			return f(rg, ps, r.URL.Query(), func(v interface{}) error {
				return api.Decode(r, v)
			})
		})
	}
}

func balance(rg *reportgenerator, ps httprouter.Params, uv url.Values, d apisupport.Decoder) (interface{}, error) {
	var at time.Time
	if err := parse(uv.Get("at")).into(&at); err != nil {
		return nil, err
	}
	return rg.BalanceSheet(at)
}

func incomeStatement(rg *reportgenerator, ps httprouter.Params, uv url.Values, d apisupport.Decoder) (interface{}, error) {
	var from, to time.Time
	if err := parse(uv.Get("from"), uv.Get("to")).into(&from, &to); err != nil {
		return nil, err
	}
	return rg.IncomeStatement(from, to)
}

func journal(rg *reportgenerator, ps httprouter.Params, uv url.Values, d apisupport.Decoder) (interface{}, error) {
	var from, to time.Time
	if err := parse(uv.Get("from"), uv.Get("to")).into(&from, &to); err != nil {
		return nil, err
	}
	return rg.Journal(from, to)
}

func ledger(rg *reportgenerator, ps httprouter.Params, uv url.Values, d apisupport.Decoder) (interface{}, error) {
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
	var err error
	api, err = apisupport.New(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	router := httprouter.New()
	router.GET("/charts-of-accounts/:coa/balance-sheet", handler(balance))
	router.GET("/charts-of-accounts/:coa/income-statement", handler(incomeStatement))
	router.GET("/charts-of-accounts/:coa/journal", handler(journal))
	router.GET("/charts-of-accounts/:coa/accounts/:accountid/ledger", handler(ledger))
	log.Fatal(http.ListenAndServe(":8080", router))
}
