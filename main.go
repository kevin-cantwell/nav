package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/nsf/termbox-go"
)

var (
	search = &searchBox{
		cursorOffsetX: 0,
		cursorOffsetY: 0,
		value:         []rune{},
	}
	results = initResultsBox()
	debug   = &debugBox{
		buf: &bytes.Buffer{},
	}
)

func initResultsBox() *resultsBox {
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

	var filepaths []string
	filepath.Walk(basepath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if path == basepath {
			return nil
		}
		if info.IsDir() {
			if info.Name() == ".git" {
				return filepath.SkipDir
			}
			filepaths = append(filepaths, path)
		}
		return nil
	})

	return &resultsBox{
		basepath:  basepath,
		filepaths: filepaths,
	}
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

	results.CalculateMatches(true)
	redrawAll()
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
				search.MoveCursorOneRuneBackward()
			case termbox.KeyArrowRight, termbox.KeyCtrlF:
				search.MoveCursorOneRuneForward()
			case termbox.KeyBackspace, termbox.KeyBackspace2:
				if ev.Mod == termbox.ModAlt {
					search.DeleteWordBackward()
				} else {
					search.DeleteRuneBackward()
				}
			case termbox.KeyDelete, termbox.KeyCtrlD:
				search.DeleteRuneForward()
			case termbox.KeySpace:
				search.InsertRune(' ')
			case termbox.KeyArrowDown:
				results.MoveSelectionDownOne()
			case termbox.KeyArrowUp:
				results.MoveSelectionUpOne()
			default:
				if ev.Ch != 0 {
					if ev.Mod == termbox.ModAlt {
						switch ev.Ch {
						case 'b':
							search.MoveCursorOneWordBackward()
						case 'f':
							search.MoveCursorOneWordForward()
						}
					} else {
						search.InsertRune(ev.Ch)
					}
				}
			}
		case termbox.EventError:
			return "", ev.Err
		}
		redrawAll()
	}
}

type debugBox struct {
	buf *bytes.Buffer
}

func (b *debugBox) Draw() {
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

type resultsBox struct {
	basepath  string
	filepaths []string
	matches   []string
	selected  int
}

func (b *resultsBox) Draw() {
	for y, path := range b.matches {
		fg, bg := termbox.ColorDefault, termbox.ColorDefault
		if y == b.selected {
			fg = termbox.ColorCyan
			bg = termbox.ColorBlack
			termbox.SetCell(0, y+3, '>', fg, bg)
		}
		for x, r := range b.displayPath(path) {
			termbox.SetCell(x+1, y+3, r, fg, bg)
		}
	}
}

func (b *resultsBox) MoveSelectionDownOne() {
	if b.selected >= len(b.matches) {
		return
	}
	b.selected++
}

func (b *resultsBox) MoveSelectionUpOne() {
	if b.selected <= 0 {
		return
	}
	b.selected--
}

func (b *resultsBox) CalculateMatches(selectBestMatch bool) {
	b.matches = nil
	if len(search.value) == 0 {
		b.matches = b.filepaths
		return
	}
	var bestScore int
	for _, path := range b.filepaths {
		partial := b.displayPath(path)
		var score int
		var i int
		for _, q := range search.value {
			partial = partial[i:]
			i = strings.IndexRune(partial, q)
			if i < 0 {
				score = -1
				break
			}
			i++
			score++
		}
		if score > 0 {
			b.matches = append(b.matches, path)
		}
		if selectBestMatch && score > bestScore {
			bestScore = score
			b.selected = len(b.matches) - 1
		}
	}
}

func (b *resultsBox) displayPath(path string) string {
	rel, err := filepath.Rel(b.basepath, path)
	if err != nil {
		panic(err)
	}
	return rel
}

func (b *resultsBox) Selected() string {
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
	cursorOffsetX int
	cursorOffsetY int
	value         []rune
}

func (b *searchBox) Draw() {
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

	for i, r := range b.value {
		termbox.SetCell(i+1, 1, r, termbox.ColorDefault, termbox.ColorDefault)
	}

	termbox.SetCursor(b.cursorOffsetX+1, b.cursorOffsetY+1)
}

func (b *searchBox) InsertRune(r rune) {
	tail := append([]rune{r}, b.value[b.cursorOffsetX:]...)
	b.value = append(b.value[:b.cursorOffsetX], tail...)
	b.cursorOffsetX++

	results.CalculateMatches(true)
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
}

func (b *searchBox) DeleteRuneBackward() {
	if b.cursorOffsetX <= 0 {
		return
	}
	b.value = append(b.value[:b.cursorOffsetX-1], b.value[b.cursorOffsetX:]...)
	b.cursorOffsetX--

	results.CalculateMatches(true)
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

	results.CalculateMatches(true)
}

func (b *searchBox) DeleteRuneForward() {
	if b.cursorOffsetX >= len(b.value) {
		return
	}
	b.value = append(b.value[:b.cursorOffsetX], b.value[b.cursorOffsetX+1:]...)

	results.CalculateMatches(true)
}

func redrawAll() {
	termbox.Clear(termbox.ColorDefault, termbox.ColorDefault)
	search.Draw()
	results.Draw()
	debug.Draw()
	termbox.Flush()
}
