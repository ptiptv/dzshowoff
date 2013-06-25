package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"text/template"

	"github.com/knieriem/markdown"
)

var (
	port      = flag.Int("port", 8080, "port for the built in webserver")
	slidesDir = flag.String("slidesRoot", ".", "root dir of slides")
)

type slide struct {
	Content string
	Notes   string
}

type viewport struct {
	Height int
	Width  int
}

type show struct {
	Title  string
	Slides []slide
	View   viewport
	Images map[string]string // map of basename to full filesystem path
}

func (v viewport) HeightHalf() int {
	return v.Height / 2
}
func (v viewport) WidthHalf() int {
	return v.Width / 2
}

type showoffsection struct {
	Section string `json:"section,omitempty"`
}

type showoffjson struct {
	Name     string           `json:"name,omitempty"`
	Sections []showoffsection `json:"sections,omitempty"`
}

func loadslides() show {
	jsonPath := path.Join(*slidesDir, "showoff.json")
	j, err := os.Open(jsonPath)
	if err != nil {
		log.Fatalf("Error opening %s: %v", jsonPath, err)
	}
	_ = markdown.APOSTROPHE

	dec := json.NewDecoder(j)
	var raw showoffjson
	err = dec.Decode(&raw)
	if err != nil {
		log.Fatalf("Error parsing json: %v", err)
	}
	allslides := bytes.NewBuffer(nil)
	images := make(map[string]string)
	for _, section := range raw.Sections {
		sectionDir := path.Join(*slidesDir, section.Section)
		files, err := ioutil.ReadDir(sectionDir)
		if err != nil {
			log.Fatalf("error reading section: %v", err)
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			n := f.Name()
			if !strings.HasSuffix(n, ".md") {
				images[n] = path.Join(sectionDir, n)
				continue
			}
			fp := path.Join(sectionDir, n)
			fd, err := os.Open(fp)
			if err != nil {
				log.Fatalf("Error opening file %v: %v", fp, err)
			}
			data, err := ioutil.ReadAll(fd)
			if err != nil {
				log.Fatalf("Error reading file %v: %v", fp, err)
			}
			allslides.Write(data)
			allslides.WriteString("\n\n")
		}
	}
	slidedata := strings.Trim(allslides.String(), " \n\r\t")
	slidestrings := strings.Split(slidedata, "!SLIDE")
	var slides []slide
	for _, s := range slidestrings[1:] {
		content, notes := htmlSlide(s)
		slides = append(slides, slide{
			Content: content,
			Notes:   notes,
		})
	}
	return show{
		Title:  raw.Name,
		Slides: slides,
		View: viewport{
			Height: 768,
			Width:  1024,
		},
		Images: images,
	}
}

// htmlSlide transforms the markdown slide source into html for a
// slide, and a string of the speaker notes for that slide.
func htmlSlide(mdown string) (string, string) {
	splits := strings.SplitN(mdown, "\n", 2)
	slidetype := strings.Trim(splits[0], " \t")
	_ = slidetype // TODO(augie): figure out how to use this data
	splits = strings.SplitN(splits[1], ".notes", 2)
	content := strings.Trim(splits[0], " \t\r\n")
	notes := ""
	if len(splits) == 2 {
		notes = strings.Trim(splits[1], " \t\r\n")
	}

	p := markdown.NewParser(&markdown.Extensions{})
	dest := bytes.NewBuffer(nil)
	p.Markdown(bytes.NewBuffer([]byte(content)), markdown.ToHTML(dest))
	rendered := dest.String()
	// TODO(augie): stop using this horrible hack for images!
	rendered = strings.Replace(rendered, "<img src=\"", "<img src=\"images/", -1)
	return rendered, notes
}

func slidehandler(w http.ResponseWriter, r *http.Request) {
	t := template.Must(template.ParseFiles("template.html"))
	err := t.Execute(w, loadslides())
	if err != nil {
		w.Write([]byte(fmt.Sprintf("Error rendering slides: %v", err)))
	}
}

func presenter(w http.ResponseWriter, r *http.Request) {
	f, err := os.Open("onstage.html")
	if err != nil {
		panic(err)
	}
	data, err := ioutil.ReadAll(f)
	if err != nil {
		panic(err)
	}
	w.Write(data)
}

func presRedir(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/presenter/#/", http.StatusFound)
}

func images(w http.ResponseWriter, r *http.Request) {
	deck := loadslides()
	components := strings.Split(r.URL.Path, "/")
	basename := components[len(components)-1]
	path := deck.Images[strings.Trim(basename, "/")]
	if path == "" {
		http.NotFound(w, r)
	}
	d, err := ioutil.ReadFile(path)
	if err != nil {
		http.Error(w, err.Error(), 500)
	}
	if strings.HasSuffix(basename, ".svg") {
		w.Header().Add("Content-Type", "image/svg+xml")
	}
	w.Write(d)
}

func serve() {
	s := http.NewServeMux()
	s.HandleFunc("/", slidehandler)
	s.HandleFunc("/p", presRedir)
	s.HandleFunc("/images/", images)
	s.HandleFunc("/presenter", presenter)
	s.HandleFunc("/presenter/", presenter)
	log.Printf("Starting webserver on http://localhost:%d", *port)
	log.Printf("Presenter display on http://localhost:%d/p", *port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), s))
}

func main() {
	flag.Parse()

	if s, err := os.Stat(path.Join(*slidesDir, "showoff.json")); os.IsNotExist(err) || s.IsDir() {
		log.Fatalf("Invalid slide dir %v", *slidesDir)
	}
	log.Printf("Loading slides from %v", *slidesDir)
	subcmd := flag.Arg(0)
	switch subcmd {
	case "serve":
		serve()
	default:
		log.Fatal("Missing a subcommand. Valid subcommands are: serve, ...")
	}
}
