package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
	//"github.com/PuerkitoBio/goquery"
)

type book struct {
	BookName           string `json:"bookName"`
	FileName           string `json:"fileName"`
	Baked              bool   `json:"baked"`
	ContentFetchedAt   string `json:"contentFetchedAt"`
	ContentFetchedFrom string `json:"contentFetchedFrom"`
}

type instance struct {
	SectionName string `json:"section_name"`
	Link        string `json:"link"`
	Instances   int    `json:"instances"`
}

type result struct {
	Results            []instance `json:"Results"`
	BookName           string     `json:"bookName"`
	FileName           string     `json:"fileName"`
	Baked              bool       `json:"baked"`
	ContentFetchedAt   string     `json:"contentFetchedAt"`
	ContentFetchedFrom string     `json:"contentFetchedFrom"`
}

func main() {
	http.HandleFunc("/", serverStatus)
	http.HandleFunc("/books", bookList)
	http.HandleFunc("/elements", handleSearch)

	if err := http.ListenAndServe(":3000", nil); err != nil {
		log.Fatalln(err)
	}
}

func serverStatus(w http.ResponseWriter, r *http.Request) {
	fmt.Println("GET /")

	message := "{\"status\": \"active\"}"
	w.Header().Add("Content-Type", "application/json")
	w.Write([]byte(message))
}

func bookList(w http.ResponseWriter, r *http.Request) {
	fmt.Println("GET /books")

	bl := books
	jsonString, _ := json.Marshal(bl)
	w.Header().Add("Content-Type", "application/json")
	w.Write([]byte(jsonString))
}

func handleSearch(w http.ResponseWriter, r *http.Request) {
	fmt.Println("GET /elements")

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
	if err != nil {
		log.Fatalln(err)
		w.WriteHeader(502)
		w.Write([]byte("Something went wrong."))
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

	fmt.Printf("Starting searching for: %v in %v", element, bookName)

	// Find pages for unbaked and baked books
	pages := doc.Find("[data-type=\"composite-page\"], [data-type=\"page\"]")
	pages.Each(func(i int, s *goquery.Selection) {
		var titleNode *goquery.Selection
		s.Find("*:not([data-type=\"metadata\"]) > [data-type=\"document-title\"]").Each(func(i int, sTN *goquery.Selection) {
			if i == 0 {
				titleNode = sTN
			}
		})
		titleNumber := titleNode.Find(".os-number").Text()
		var title string
		if titleNumber == "" {
			var chapterTitle string
			var chapterNumber string

			// Chapter title is used to specific region of search for unnumbered pages
			titleNode.Parent().Parent().Find("*:not([data-type=\"metadata\"]) > h1[data-type=\"document-title\"]").Each(func(i int, sT *goquery.Selection) {
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
			} else {
				title = "Chapter: " + chapterTitle + "; Module: " + titleNode.Text()
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

			s.Find(el).Each(func(i int, sEl *goquery.Selection) {
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

			s.Find(leftEl).Each(func(i int, sEl *goquery.Selection) {
				if len(sEl.Find(rightEl).Nodes) > 0 {
					ins++
				}
			})
		} else {
			ins = len(s.Find(element).Nodes)
		}

		if ins > 0 {
			res = append(res, instance{SectionName: title, Link: "", Instances: ins})
		}
	})

	return res, nil
}

func trimUseless(s string) string {
	rep := strings.NewReplacer("\"", "", "'", "", "(", "", ")", "")
	return rep.Replace(s)
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
