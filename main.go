package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// Create custom book struct which holds all data about book.
type book struct {
	BookName           string `json:"bookName"`
	FileName           string `json:"fileName"`
	Baked              bool   `json:"baked"`
	ContendBakedAt     string `json:"contentBakedAt"`
	ContentUUID        string `json:"contentUUID"`
	ContentFetchedAt   string `json:"contentFetchedAt"`
	ContentFetchedFrom string `json:"contentFetchedFrom"`
}

// Create struct for storing informations about found instances at every page
type instance struct {
	SectionName string `json:"section_name"`
	Link        string `json:"link"`
	Instances   int    `json:"instances"`
}

// Create struct for success response for GEt /elements?bookName=..&element=..
type result struct {
	Results            []instance `json:"results"`
	BookName           string     `json:"bookName"`
	FileName           string     `json:"fileName"`
	Baked              bool       `json:"baked"`
	ContentFetchedAt   string     `json:"contentFetchedAt"`
	ContentFetchedFrom string     `json:"contentFetchedFrom"`
}

const (
	jobLimit   int    = 2
	timeFormat string = "2006-01-02T15:04:05+07:00"
)

func main() {
	port := ":3000"
	if len(os.Args) > 1 {
		port = ":" + os.Args[1]
	}

	// Clean limits for visitors
	go cleanupVisitors()

	// Initial jobCounter
	jobCounter := 0

	http.HandleFunc("/", serverStatus)
	http.HandleFunc("/books", bookList)
	http.HandleFunc("/elements", func(w http.ResponseWriter, r *http.Request) {
		handleSearch(w, r, &jobCounter)
	})

	t := time.Now().Format(timeFormat)
	fmt.Println(t, "Starting at port", port)

	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalln(err)
	}
}

func serverStatus(w http.ResponseWriter, r *http.Request) {
	t := time.Now().Format(timeFormat)
	fmt.Println(t, "GET /")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Add("Content-Type", "application/json")

	message := "{\"status\": \"active\"}"

	w.Write([]byte(message))
}

func bookList(w http.ResponseWriter, r *http.Request) {
	t := time.Now().Format(timeFormat)
	fmt.Println(t, "GET /books")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Add("Content-Type", "application/json")

	bl := books
	jsonString, _ := json.Marshal(bl)

	w.Write([]byte(jsonString))
}

func handleSearch(w http.ResponseWriter, r *http.Request, jc *int) {
	t := time.Now().Format(timeFormat)
	fmt.Println(t, "GET /elements")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")

	// r.RemoteAddr >> IP:PORT < we will limit users by ip without port
	limiter := getVisitor(strings.SplitN(r.RemoteAddr, ":", -1)[0])
	if limiter.Allow() == false {
		w.WriteHeader(429)
		w.Write([]byte("To many requests."))
		return
	}

	// Limit number of jobs which can run at once
	if *jc >= jobLimit {
		w.WriteHeader(502)
		w.Write([]byte("Other job is running."))
		return
	}

	// Add current job to counter
	(*jc)++

	params := r.URL.Query()

	if params["bookName"] == nil || params["element"] == nil {
		w.WriteHeader(502)
		w.Write([]byte("Please provide bookName and element parameters."))
		return
	}

	rep := strings.NewReplacer("_", " ")

	bookName := rep.Replace(params["bookName"][0])
	element := params["element"][0]

	res, err := findElements(bookName, element)

	// Remove current job from counter
	(*jc)--

	if err != nil {
		fmt.Println(err)
		w.WriteHeader(502)
		w.Write([]byte(err.Error()))
		return
	}

	b := books[bookName]
	response := result{
		Results:            res,
		BookName:           b.BookName,
		FileName:           b.FileName,
		Baked:              b.Baked,
		ContentFetchedAt:   b.ContentFetchedAt,
		ContentFetchedFrom: b.ContentFetchedFrom,
	}

	jsonString, _ := json.Marshal(response)

	w.Header().Add("Content-Type", "application/json")
	w.Write([]byte(jsonString))
}

func findElements(bookName string, element string) ([]instance, error) {
	b := books[bookName]

	res := []instance{}

	filename := b.FileName
	if filename == "" {
		e := errors.New("Couldn't find file for " + bookName)
		return res, e
	}

	p := "./books/" + filename
	bs, err := ioutil.ReadFile(p)
	if err != nil {
		e := errors.New("Error while opening book file in " + p)
		return res, e
	}

	c := strings.NewReader(string(bs))
	doc, err := goquery.NewDocumentFromReader(c)
	if err != nil {
		e := errors.New("Error while parsing xhtml from file " + p)
		return res, e
	}

	fmt.Printf("Starting searching for: %v in %v \n", element, bookName)

	// Find pages for unbaked and baked books
	pages := doc.Find("[data-type=\"composite-page\"], [data-type=\"page\"]")
	pages.Each(func(i int, s *goquery.Selection) {
		var titleNode *goquery.Selection
		s.Find("*:not([data-type=\"metadata\"]) > [data-type=\"document-title\"], *:not([data-type=\"metadata\"]) > [data-type=\"title\"]").Each(func(i int, sTN *goquery.Selection) {
			if i == 0 {
				titleNode = sTN
			}
		})

		if titleNode == nil {
			fmt.Printf("Couldn't find title for page with index %v\n", i)
			return
		}

		titleNumber := titleNode.Find(".os-number").Text()

		var title string
		if titleNumber == "" {
			var chapterTitle string
			var chapterNumber string

			// Chapter title is used to specific region of search for unnumbered pages
			titleNode.Parent().Parent().Find("h1[data-type=\"document-title\"]").Each(func(i int, sT *goquery.Selection) {
				if i == 0 {
					chapterTitle = sT.Text()
					sT.Find(".os-number").Each(func(i int, sTN *goquery.Selection) {
						if i == 0 {
							chapterNumber = sTN.Text()
						}
					})
					return
				}
			})

			if chapterNumber != "" {
				title = chapterNumber + " " + titleNode.Text()
			} else if chapterTitle != "" && chapterTitle != "Preface" {
				title = "Chapter: " + chapterTitle + "; Module: " + titleNode.Text()
			} else {
				title = titleNode.Text()
			}
		} else {
			title = titleNode.Text()
		}

		// Declare counter for instances
		ins := 0

		if strings.Contains(element, ":hasText(") {
			// Support for custom selector element:hasTexT(text)
			sp := strings.SplitN(element, ":hasText", -1)
			if len(sp) != 2 {
				log.Fatalln("Error while proessing :hasText() selector.")
				return
			}

			el := sp[0]
			text := trimUseless(sp[1])

			s.Find(":not([data-type=\"metadata\"]) > " + el).Each(func(i int, sEl *goquery.Selection) {
				if strings.Contains(sEl.Text(), text) {
					ins++
				}
			})
		} else if strings.Contains(element, ":has(") {
			// Support for custom selector element:has(element)
			sp := strings.SplitN(element, ":has", -1)
			if len(sp) != 2 {
				log.Fatalln("Error while processing :has() selector.")
				return
			}

			leftEl := sp[0]
			rightEl := trimUseless(sp[1])

			s.Find(":not([data-type=\"metadata\"]) > " + leftEl).Each(func(i int, sEl *goquery.Selection) {
				if len(sEl.Find(rightEl).Nodes) > 0 {
					ins++
				}
			})
		} else {
			ins = len(s.Find(":not([data-type=\"metadata\"]) > " + element).Nodes)
		}

		if ins > 0 {
			res = append(res, instance{SectionName: title, Link: "", Instances: ins})
		}
	})

	return res, nil
}

// It takes (text) from :hasText(text) and returns stripped text.
func trimUseless(s string) string {
	sliced := s[1 : len(s)-1]                     // remove ()
	rep := strings.NewReplacer("\"", "", "'", "") // remove " and '
	return rep.Replace(sliced)
}

func splitAtHasText(s string) (el string, text string, err error) {
	sp := strings.SplitN(s, ":hasText", -1)
	if len(sp) > 2 {
		return "", "", errors.New("We do not support nested :hasText() selectors")
	}
	return sp[0], trimUseless(sp[1]), nil
}

func splitAtHas(s string) (leftEl string, rightEl string, err error) {
	sp := strings.SplitN(s, ":has", -1)
	if len(sp) != 2 {
		return "", "", errors.New("We do not support nested :has() selectors")
	}
	return sp[0], trimUseless(sp[1]), nil
}

// TODO Ogarnąć dlaczego w calculus są złe nazwy rozdziałów
// TODO SplitAtHas/Text przed pętlą, żeby się nie powtarzać
