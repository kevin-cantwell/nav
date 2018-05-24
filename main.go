package main

import (
	"bytes"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/nsf/termbox-go"
)

var (
	search = &searchBox{
		basepath:      initBasepath(),
		cursorOffsetX: 0,
		cursorOffsetY: 0,
		value:         []rune{},
	}
	results = &resultsBox{}
	debug   = &debugBox{
		buf: &bytes.Buffer{},
	}
)

func initBasepath() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	basepath := wd
	for ; basepath != "/"; basepath = filepath.Dir(basepath) {
		// keep walking up the tree until we find a git root
		info, err := os.Stat(filepath.Join(basepath, ".git"))
		if err == nil && info.IsDir() {
			break
		}
	}
	if basepath == "/" {
		basepath = wd
	}
	return basepath
}

func main() {
	log.SetOutput(debug)
	log.SetFlags(0)

	result, err := run()
	if err != nil {
		panic(err)
	}
	os.Stdout.WriteString(result)
}

func run() (string, error) {
	err := termbox.Init()
	if err != nil {
		panic(err)
	}
	termbox.SetInputMode(termbox.InputAlt)
	defer termbox.Close()

	go results.Init()

	for {
		switch ev := termbox.PollEvent(); ev.Type {
		case termbox.EventKey:
			switch ev.Key {
			case termbox.KeyEnter:
				return results.Selected(), nil
			case termbox.KeyEsc:
				return ".", nil
			case termbox.KeyCtrlC:
				return ".", nil
			case termbox.KeyArrowLeft, termbox.KeyCtrlB:
				go search.MoveCursorOneRuneBackward()
			case termbox.KeyArrowRight, termbox.KeyCtrlF:
				go search.MoveCursorOneRuneForward()
			case termbox.KeyBackspace, termbox.KeyBackspace2:
				if ev.Mod == termbox.ModAlt {
					go search.DeleteWordBackward()
				} else {
					go search.DeleteRuneBackward()
				}
			case termbox.KeyDelete, termbox.KeyCtrlD:
				go search.DeleteRuneForward()
			case termbox.KeySpace:
				go search.InsertRune(' ')
			case termbox.KeyArrowDown:
				go results.MoveSelectionDownOne()
			case termbox.KeyArrowUp:
				go results.MoveSelectionUpOne()
			default:
				if ev.Ch != 0 {
					if ev.Mod == termbox.ModAlt {
						switch ev.Ch {
						case 'b':
							go search.MoveCursorOneWordBackward()
						case 'f':
							go search.MoveCursorOneWordForward()
						}
					} else {
						go search.InsertRune(ev.Ch)
					}
				}
			}
		case termbox.EventError:
			return "", ev.Err
		}
	}
}

type resultsBox struct {
	matches  []string
	selected int

	mu        sync.Mutex
	filepaths []string
}

func readirs(dirname string, filepaths chan<- []string) {
	infos, err := ioutil.ReadDir(dirname)
	if err != nil {
		return
	}
	var dirpaths []string
	for _, info := range infos {
		if info.IsDir() {
			subdir := filepath.Join(dirname, info.Name())
			dirpaths = append(dirpaths, subdir)
			go readirs(subdir, filepaths)
		}
	}
	if len(dirpaths) > 0 {
		filepaths <- dirpaths
	}
}

func (b *resultsBox) Init() {
	b.AppendFilepaths([]string{search.basepath})

	dirs := make(chan []string)

	go readirs(search.basepath, dirs)

	for filepaths := range dirs {
		b.AppendFilepaths(filepaths)
		redrawAll()
	}
}

func (b *resultsBox) Draw() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for y, path := range b.matches {
		fg, bg := termbox.ColorDefault, termbox.ColorDefault
		if y == b.selected {
			termbox.SetCell(0, y+3, '►', fg, bg)
			fg = termbox.AttrBold | termbox.AttrUnderline
		}
		for x, r := range search.displayPath(path) {
			termbox.SetCell(x+2, y+3, r, fg, bg)
		}
	}
}

func (b *resultsBox) MoveSelectionDownOne() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.selected >= len(b.matches)-1 {
		return
	}
	b.selected++

	redrawAll()
}

func (b *resultsBox) MoveSelectionUpOne() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.selected <= 0 {
		return
	}
	b.selected--

	redrawAll()
}

func (b *resultsBox) AppendFilepaths(filepaths []string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	a := b
	_ = a

	all := append(b.filepaths, filepaths...)
	sort.Strings(all)
	b.filepaths = all

	go b.Recalculate()
}

func (b *resultsBox) Recalculate() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.matches = nil
	for _, filepath := range b.filepaths {
		score := search.Score(filepath)
		if score > 0 {
			b.matches = append(b.matches, filepath)
		}
	}
	if b.selected >= len(b.matches) {
		b.selected = len(b.matches) - 1
	}
	redrawAll()
}

func (b *resultsBox) SelectBestMatch() {
	b.mu.Lock()
	defer b.mu.Unlock()

	var bestScore int
	for i, match := range b.matches {
		score := search.Score(match)
		if score > bestScore {
			bestScore = score
			b.selected = i
		}
	}

	redrawAll()
}

func (b *resultsBox) Selected() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.selected < 0 || len(b.matches) == 0 {
		return "."
	}
	return b.matches[b.selected]
}

func delim(r rune) bool {
	switch r {
	case '\\', '/', ' ', '.', '\t', ',', '-', '|':
		return true
	}
	return false
}

func word(r rune) bool {
	return !delim(r)
}

type searchBox struct {
	basepath      string
	cursorOffsetX int
	cursorOffsetY int
	value         []rune

	mu sync.Mutex
}

func (b *searchBox) Draw() {
	b.mu.Lock()
	defer b.mu.Unlock()

	label := b.basepath + string(filepath.Separator)
	w, _ := termbox.Size()
	termbox.SetCell(0, 0, '┌', termbox.ColorDefault, termbox.ColorDefault)
	termbox.SetCell(0, 1, '│', termbox.ColorDefault, termbox.ColorDefault)
	termbox.SetCell(0, 2, '└', termbox.ColorDefault, termbox.ColorDefault)
	for i := 1; i < w-1; i++ {
		termbox.SetCell(i, 0, '─', termbox.ColorDefault, termbox.ColorDefault)
		termbox.SetCell(i, 2, '─', termbox.ColorDefault, termbox.ColorDefault)
	}
	termbox.SetCell(w-1, 0, '┐', termbox.ColorDefault, termbox.ColorDefault)
	termbox.SetCell(w-1, 1, '│', termbox.ColorDefault, termbox.ColorDefault)
	termbox.SetCell(w-1, 2, '┘', termbox.ColorDefault, termbox.ColorDefault)

	for i, r := range label {
		termbox.SetCell(i+1, 1, r, termbox.AttrBold, termbox.ColorDefault)
	}
	for i, r := range b.value {
		termbox.SetCell(len(label)+i+1, 1, r, termbox.ColorDefault, termbox.ColorDefault)
	}

	termbox.SetCursor(len(label)+b.cursorOffsetX+1, b.cursorOffsetY+1)
}

func (b *searchBox) Score(filepath string) int {
	// everything matches an empty query equally
	if len(b.value) == 0 {
		return 1
	}
	partial := b.displayPath(filepath)
	var score int
	var i int
	for _, q := range search.value {
		partial = partial[i:]
		i = strings.IndexRune(partial, q)
		if i < 0 {
			return 0
		}
		i++
		score++
	}
	return score
}

func (b *searchBox) displayPath(path string) string {
	rel, err := filepath.Rel(b.basepath, path)
	if err != nil {
		panic(err)
	}
	return rel
}

func (b *searchBox) InsertRune(r rune) {

	tail := append([]rune{r}, b.value[b.cursorOffsetX:]...)
	b.value = append(b.value[:b.cursorOffsetX], tail...)
	b.cursorOffsetX++

	go func() {
		results.Recalculate()
		results.SelectBestMatch()
	}()

	redrawAll()
}

func (b *searchBox) MoveCursorOneRuneBackward() {
	if b.cursorOffsetX <= 0 {
		return
	}
	b.cursorOffsetX--
}

func (b *searchBox) MoveCursorOneRuneForward() {
	if b.cursorOffsetX >= len(b.value) {
		return
	}
	b.cursorOffsetX++

	redrawAll()
}

func (b *searchBox) MoveCursorOneWordBackward() {
	if b.cursorOffsetX <= 0 {
		return
	}

	prefix := string(b.value[:b.cursorOffsetX])

	// trim all delims then one word
	prefix = strings.TrimRightFunc(prefix, delim)
	prefix = strings.TrimRightFunc(prefix, word)

	b.cursorOffsetX = len([]rune(prefix))

	redrawAll()
}

func (b *searchBox) MoveCursorOneWordForward() {
	if b.cursorOffsetX >= len(b.value) {
		return
	}

	suffix := string(b.value[b.cursorOffsetX:])

	// trim all delims then one word
	suffix = strings.TrimLeftFunc(suffix, delim)
	suffix = strings.TrimLeftFunc(suffix, word)

	b.cursorOffsetX = len(b.value) - len([]rune(suffix))

	redrawAll()
}

func (b *searchBox) DeleteRuneBackward() {
	if b.cursorOffsetX <= 0 {
		return
	}
	b.value = append(b.value[:b.cursorOffsetX-1], b.value[b.cursorOffsetX:]...)
	b.cursorOffsetX--

	go func() {
		results.Recalculate()
		results.SelectBestMatch()
	}()

	redrawAll()
}

func (b *searchBox) DeleteWordBackward() {
	if b.cursorOffsetX <= 0 {
		return
	}

	prefix := string(b.value[:b.cursorOffsetX])
	suffix := string(b.value[b.cursorOffsetX:])

	// trim all delims then one word
	prefix = strings.TrimRightFunc(prefix, delim)
	prefix = strings.TrimRightFunc(prefix, word)
	b.value = []rune(prefix + suffix)
	b.cursorOffsetX = len([]rune(prefix))

	go func() {
		results.Recalculate()
		results.SelectBestMatch()
	}()

	redrawAll()
}

func (b *searchBox) DeleteRuneForward() {
	if b.cursorOffsetX >= len(b.value) {
		return
	}
	b.value = append(b.value[:b.cursorOffsetX], b.value[b.cursorOffsetX+1:]...)

	go func() {
		results.Recalculate()
		results.SelectBestMatch()
	}()

	redrawAll()
}

var drawMutex sync.Mutex

func redrawAll() {
	go func() {
		drawMutex.Lock()
		defer drawMutex.Unlock()

		termbox.Clear(termbox.ColorDefault, termbox.ColorDefault)
		search.Draw()
		results.Draw()
		debug.Draw()
		termbox.Flush()
	}()
}

type debugBox struct {
	buf *bytes.Buffer

	mu sync.Mutex
}

func (b *debugBox) Draw() {
	b.mu.Lock()
	defer b.mu.Unlock()

	defer b.buf.Reset()

	if os.Getenv("DEBUG") == "" {
		return
	}

	lines := strings.Split(string(b.buf.Bytes()), "\n")

	w, h := termbox.Size()
	for i := 0; i < w; i++ {
		termbox.SetCell(i, h-len(lines)-1, '─', termbox.ColorDefault, termbox.ColorDefault)
	}
	for y, line := range lines {
		for x, r := range line {
			termbox.SetCell(x, h-len(lines)+y, r, termbox.ColorDefault, termbox.ColorDefault)
		}
	}

}

func (b *debugBox) Write(p []byte) (int, error) {
	return b.buf.Write(p)
}
