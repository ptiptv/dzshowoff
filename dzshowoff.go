package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"text/template"

	"github.com/russross/blackfriday"

	"hg.durin42.com/dzshowoff/templates"
	"hg.durin42.com/dzshowoff/third_party/shjs"
)

var (
	port      = flag.Int("port", 8080, "port for the built in webserver")
	slidesDir = flag.String("slidesRoot", ".", "root dir of slides")
)

const (
	showoffCss = `
.bullets > ul {
	list-style: none;
}
.bullets > ul > li {
	text-align: center;
}

.innerContent ul ul {
	list-style: disc;
	text-align: left;
}

.bullets ul ul > li { font-size: 80%; }

li {
	font-size: 150%;
	margin-left: 0.5em;
}

h1 {
	margin-top: 0px;
}
section > div {
	vertical-align: middle;
	display: table-cell;
	height: {{.View.Height}}px;
	width: {{.View.Width}}px;
}
`

	showoffExtraHead = `
<script type="text/javascript" src="/shjs/sh_main.min.js"></script>
<link type="text/css" rel="stylesheet" href="/shjs/css/sh_emacs.min.css">
`

	showoffOnload = "sh_highlightDocument('shjs/lang/', '.min.js');"
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
	Title     string
	Slides    []slide
	View      viewport
	Images    map[string]string // map of basename to full filesystem path
	Css       string            //extra CSS to inject into template
	ExtraHead string            // extra junk to put inside the head element
	Onload    string            // javascript to run after the dzslides js, if any
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
	View     viewport         `json:"view,omitempty"`
}

func loadslides() show {
	jsonPath := path.Join(*slidesDir, "showoff.json")
	j, err := os.Open(jsonPath)
	defer j.Close()
	if err != nil {
		log.Fatalf("Error opening %s: %v", jsonPath, err)
	}

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
			defer fd.Close()
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
	view := viewport{
		Height: 768,
		Width:  1024,
	}
	if raw.View.Height != 0 && raw.View.Width != 0 {
		view = raw.View
	}
	t := template.New("inline css")
	css := template.Must(t.Parse(showoffCss))
	cssBuf := bytes.NewBuffer(nil)
	err = css.Execute(cssBuf, struct{ View viewport }{View: view})
	if err != nil {
		panic(fmt.Errorf("Fatal error rendering inline CSS: %v", err))
	}
	finalCss, err := ioutil.ReadAll(cssBuf)
	return show{
		Title:     raw.Name,
		Slides:    slides,
		View:      view,
		Images:    images,
		Css:       string(finalCss),
		ExtraHead: showoffExtraHead,
		Onload:    showoffOnload,
	}
}

var (
	renderedCodeBlock = regexp.MustCompile("<pre><code>@@@( *[^\n]*)\n")
)

// htmlSlide transforms the markdown slide source into html for a
// slide, and a string of the speaker notes for that slide.
func htmlSlide(mdown string) (string, string) {
	splits := strings.SplitN(mdown, "\n", 2)
	slidetype := strings.Trim(splits[0], " \t")
	style := ""
	class := ""
	switch slidetype {
	case "":
		class = "default"
		style += " padding: 2em; "
	case "center":
		class = "center"
		style += "text-align: center; "
	case "bullets":
		class = "bullets"
	default:
		panic(fmt.Errorf("invalid slide type %v", slidetype))
	}
	splits = strings.SplitN(splits[1], ".notes", 2)
	content := splits[0]
	notes := ""
	if len(splits) == 2 {
		notes = strings.Trim(splits[1], " \t\r\n")
	}
	postProcessCodeBlocks := strings.Contains(content, "    @@@")

	r := blackfriday.HtmlRenderer(blackfriday.HTML_OMIT_CONTENTS|blackfriday.HTML_SKIP_STYLE, "", "")
	rendered := string(blackfriday.Markdown([]byte(content), r, 0))
	// TODO(augie): stop using this horrible hack for images!
	rendered = strings.Replace(rendered, "<img src=\"", "<img src=\"images/", -1)
	rendered = fmt.Sprintf("<div class=\"%s innerContent\" style=\"%s\">%s</div>",
		class, style, rendered)
	// TODO(augie): stop using this horrible hack for code blocks!
	if postProcessCodeBlocks {
		rendered = renderedCodeBlock.ReplaceAllStringFunc(rendered, func(match string) string {
			parts := strings.SplitN(match, "@@@", 2)
			lang := ""
			if len(parts) > 1 {
				lang = strings.TrimSpace(parts[1])
				return fmt.Sprintf("<pre class=\"sh_%s sh_sourceCode\"><code>\n", lang)
			}
			return "<pre class=\"sh_sourceCode\"><code>\n"
		})
	}
	return rendered, notes
}

func slidehandler(w http.ResponseWriter, r *http.Request) {
	err := rendershow(w, loadslides())
	if err != nil {
		w.Write([]byte(fmt.Sprintf("Error rendering slides: %v", err)))
	}
}

func rendershow(w io.Writer, deck show) error {
	t := template.New("template.html")
	t, err := t.Parse(string(templates.Files["template.html"]))
	if err != nil {
		return err
	}
	return t.Execute(w, deck)
}

func maybeDie(e error) {
	if e != nil {
		panic(e)
	}
}

func archiveHandler(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if err := recover(); err != nil {
			http.Error(w, fmt.Sprintf("%v", err), http.StatusInternalServerError)
		}
	}()
	deck := loadslides()
	w.Header().Add("Content-Disposition",
		fmt.Sprintf("attachment; filename=\"%s.zip\"", deck.Title))
	zw := zip.NewWriter(w)
	defer zw.Close()
	fw, err := zw.Create("index.html")
	maybeDie(err)
	err = rendershow(fw, deck)
	maybeDie(err)
	for name, fullpath := range deck.Images {
		fw, err := zw.Create(path.Join("images", name))
		maybeDie(err)
		r, err := os.Open(fullpath)
		maybeDie(err)
		_, err = io.Copy(fw, r)
		maybeDie(err)
	}
}

func presenter(w http.ResponseWriter, r *http.Request) {
	w.Write(templates.Files["onstage.html"])
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

// TODO(augie): replace this with an http.FileSystem implementation in shjs.go
type shjsServer struct{}

func (s *shjsServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fn := r.URL.Path

	data := shjs.Files[fn]
	if len(data) > 0 {
		if strings.HasSuffix(fn, ".css") {
			w.Header().Add("Content-Type", "text/css")
		}
		w.Write(data)
	}
}

func serve() {
	s := http.NewServeMux()
	shpath := "/shjs/"
	s.Handle(shpath, http.StripPrefix(shpath, &shjsServer{}))
	s.HandleFunc("/", slidehandler)
	s.HandleFunc("/p", presRedir)
	s.HandleFunc("/images/", images)
	s.HandleFunc("/presenter", presenter)
	s.HandleFunc("/presenter/", presenter)
	s.HandleFunc("/archive", archiveHandler)
	s.HandleFunc("/archive/", archiveHandler)
	log.Printf("Starting webserver on http://localhost:%d", *port)
	log.Printf("Presenter display on http://localhost:%d/p", *port)
	log.Printf("Archive available from http://localhost:%d/archive", *port)
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
