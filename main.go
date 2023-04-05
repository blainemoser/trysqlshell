package main

import (
	"log"

	trysql "github.com/blainemoser/TrySql"
	"github.com/blainemoser/trysqlshell/shell"
)

func main() {
	ts, err := trysql.Initialise([]string{})
	if err != nil {
		log.Fatal(err.Error())
	}
	c := shell.New(ts)
	c.Start(false)
	err = ts.TearDown()
	if err != nil {
		log.Fatal(err.Error())
	}
}
