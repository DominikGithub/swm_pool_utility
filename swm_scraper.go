package main

/*
Setup local db
--------------
sqlite3 swm_pool_utility.db "VACUUM;"
sqlite3 swm_pool_utility.db "create table track_pools(id integer primary key AUTOINCREMENT, name varchar, dtime datetime default current_timestamp, utility int);"
sqlite3 swm_pool_utility.db "drop table track_pools;"
*/

import (
    "fmt"
    "log"
	"bytes"
	"time"
	"strings"
	"strconv"
	"flag"
    "github.com/PuerkitoBio/goquery"
	"github.com/chromedp/chromedp"
	"context"
    "database/sql"
    _ "github.com/mattn/go-sqlite3"
)

func log_to_db(pool_stats map[string]int) {
	// Write to SQLite database
    db, err := sql.Open("sqlite3", "./swm_pool_utility.db")
    if err != nil {
        log.Fatal(err)
        return
    }

	for n, u := range pool_stats {
		fmt.Println(n, ">", u)
		sql := fmt.Sprintf("INSERT INTO track_pools (name, utility) VALUES ('%s', %d)", n, u)
		_, err := db.Exec(sql)
		if err != nil {
        	log.Fatal(err)
			panic(err)
			return
		}
	}
	defer db.Close()
}


type PoolStatus struct {
    id int
    name string
	dtime string
    utility int
}

func show_db_content() ([]PoolStatus, error) {
	// Read SQLite db
    db, err := sql.Open("sqlite3", "./swm_pool_utility.db")
    if err != nil {
        log.Fatal(err)
        return nil, err
    }
	defer db.Close()

	rows, err := db.Query("SELECT * FROM track_pools")
    if err != nil {
        log.Fatal(err)
    }
    defer rows.Close()

	var db_all []PoolStatus
	for rows.Next() {
        var c PoolStatus
        if err := rows.Scan(&c.id, &c.name, &c.dtime, &c.utility); err != nil {
            log.Fatal(err)
        }
        db_all = append(db_all, c)
    }

	// Check for errors from iteration
    if err := rows.Err(); err != nil {
        log.Fatal(err)
    }

    // Print all results
    // fmt.Println("DB all entries:")
    // for _, c := range db_all {
    //     fmt.Printf("%+v\n", c)
    // }

	return db_all, nil
}


func main() {
	webMode := flag.Bool("web", false, "Start web server mode")
	flag.Parse()

	if *webMode {
		startWebServer()
		return
	}

    ctx, cancel := chromedp.NewContext(context.Background())
    defer cancel()

    // load and render
    var html string
    err := chromedp.Run(ctx,
        chromedp.Navigate("https://www.swm.de/baeder/auslastung"),
		chromedp.WaitVisible(`#bad`, chromedp.ByID),
		chromedp.Sleep(2000 * time.Millisecond),
        chromedp.OuterHTML("html", &html),
    )
    if err != nil {
        log.Fatal(err)
    }

    // extract utilization
    doc, err := goquery.NewDocumentFromReader(bytes.NewReader([]byte(html)))
    if err != nil {
        log.Fatal(err)
    }

	pool_stats := make(map[string]int) 

    doc.Find("div#bad ul.bath-capacity__item-list").Each(func(i int, ul *goquery.Selection) {
		bad_name := ul.Find("h3.headline-s").Text()
		utility_str := ul.Find("div.bath-capacity-item--progress-bar__description").Text()
		utility_str = strings.Split(utility_str,`%`)[0]
		utility_str = strings.TrimSpace(utility_str)

		// stop if only empty pools detected
		utility, err := strconv.Atoi(utility_str)
		if err != nil {
			log.Fatal(err)
			panic(err)
		}

		// check extracted numbers for validity
		if utility < 1{
			panic("Failed render actualy utility. Stop!")
		}
		pool_stats[bad_name] =  utility
		//fmt.Println(bad_name, ">", utility)
    })

	log_to_db(pool_stats)
	db_all, _ := show_db_content()
	fmt.Println(" #######################")
	for n, u := range db_all {
		fmt.Println(n, ">", u)
	}

}