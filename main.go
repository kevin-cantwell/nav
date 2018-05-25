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

type evType int

const (
	EventMoveCursorForwardOneRune evType = iota
	EventMoveCursorBackwardOneRune
	EventMoveCursorForwardOneWord
	EventMoveCursorBackwardOneWord
	EventDeleteRuneForward
	EventDeleteRuneBackward
	EventDeleteWordBackward
	EventInsertRune
	EventMoveSelectionDownOne
	EventMoveSelectionUpOne
	EventSelected

	EventMouseDrag
	EventMouseClick

	EventShutdown
	EventError
)

type event struct {
	evType evType
	ch     rune
	mouseX int
	mouseY int
	err    error
}

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

	if err := termbox.Init(); err != nil {
		panic(err)
	}
	// Kill program with CtrlC
	termbox.SetInputMode(termbox.InputAlt | termbox.InputMouse)

	eventCh := make(chan event)

	go pollEvents(eventCh)

	result, err := run(eventCh)
	if err != nil {
		panic(err)
	}

	termbox.Close()
	os.Stdout.WriteString(result)
}

func pollEvents(eventCh chan<- event) {
	var prevEv termbox.Event
	var wasDraggedHere bool
	for {
		func() {
			ev := termbox.PollEvent()
			defer func() {
				prevEv = ev
			}()
			if ev.Type == termbox.EventError {
				eventCh <- event{evType: EventError, err: ev.Err}
				return
			}

			// Mouse events
			if ev.Type == termbox.EventMouse {
				switch ev.Key {
				case termbox.MouseRelease:
					if !wasDraggedHere {
						eventCh <- event{evType: EventMouseClick, mouseX: ev.MouseX, mouseY: ev.MouseY}
					}
				case termbox.MouseWheelDown:
				case termbox.MouseWheelUp:
				case termbox.MouseLeft:
					wasDraggedHere = prevEv.Key == termbox.MouseLeft && prevEv.MouseY != ev.MouseY
					eventCh <- event{evType: EventMouseDrag, mouseX: ev.MouseX, mouseY: ev.MouseY}
				default:
				}
			}

			// Keyboard events
			if ev.Type == termbox.EventKey {
				switch ev.Key {
				case termbox.KeyEnter:
					eventCh <- event{evType: EventSelected}
					return
				case termbox.KeyEsc, termbox.KeyCtrlC:
					eventCh <- event{evType: EventShutdown}
					return
				case termbox.KeyArrowLeft, termbox.KeyCtrlB:
					eventCh <- event{evType: EventMoveCursorBackwardOneRune}
				case termbox.KeyArrowRight, termbox.KeyCtrlF:
					eventCh <- event{evType: EventMoveCursorForwardOneRune}
				case termbox.KeyBackspace, termbox.KeyBackspace2:
					if ev.Mod == termbox.ModAlt {
						eventCh <- event{evType: EventDeleteWordBackward}
					} else {
						eventCh <- event{evType: EventDeleteRuneBackward}
					}
				case termbox.KeyDelete, termbox.KeyCtrlD:
					eventCh <- event{evType: EventDeleteRuneForward}
				case termbox.KeySpace:
					eventCh <- event{evType: EventInsertRune, ch: ' '}
				case termbox.KeyArrowDown:
					eventCh <- event{evType: EventMoveSelectionDownOne}
				case termbox.KeyArrowUp:
					eventCh <- event{evType: EventMoveSelectionUpOne}
				default:
					if ev.Ch != 0 {
						if ev.Mod == termbox.ModAlt {
							switch ev.Ch {
							case 'b':
								eventCh <- event{evType: EventMoveCursorBackwardOneWord}
							case 'f':
								eventCh <- event{evType: EventMoveCursorForwardOneWord}
							}
						} else {
							eventCh <- event{evType: EventInsertRune, ch: ev.Ch}
						}
					}
				}
			}
		}()
	}
}

func run(eventCh chan event) (string, error) {
	go results.Init()

	for ev := range eventCh {
		switch ev.evType {
		case EventSelected:
			return results.Selected(), nil
		case EventShutdown:
			return ".", nil // TODO: os.Exit?
		case EventError:
			return ".", ev.err
		}

		go func(ev event) {
			switch ev.evType {
			case EventInsertRune:
				search.InsertRune(ev.ch)
			case EventMoveCursorBackwardOneRune:
				search.MoveCursorOneRuneBackward()
			case EventMoveCursorBackwardOneWord:
				search.MoveCursorOneWordBackward()
			case EventMoveCursorForwardOneRune:
				search.MoveCursorOneRuneForward()
			case EventMoveCursorForwardOneWord:
				search.MoveCursorOneWordForward()
			case EventDeleteRuneBackward:
				search.DeleteRuneBackward()
			case EventDeleteRuneForward:
				search.DeleteRuneForward()
			case EventDeleteWordBackward:
				search.DeleteWordBackward()
			case EventMoveSelectionDownOne:
				results.MoveSelectionDownOne()
			case EventMoveSelectionUpOne:
				results.MoveSelectionUpOne()
			case EventMouseDrag:
				// search.MouseClick(ev.mouseX, ev.mouseY)
				results.MouseDrag(ev.mouseY)
			case EventMouseClick:
				results.MouseClick(ev.mouseX, ev.mouseY, eventCh)
			}
			redrawAll()
		}(ev)
	}

	return ".", nil
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

func (b *resultsBox) MouseDrag(y int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if y-3 < 0 || y-3 >= len(b.matches) {
		return
	}
	b.selected = y - 3
}

func (b *resultsBox) MouseClick(x, y int, eventCh chan<- event) {
	if y-3 != b.selected {
		return
	}
	if x-2 < 0 || x-2 >= len([]rune(search.displayPath(b.matches[b.selected]))) {
		return
	}
	eventCh <- event{evType: EventSelected}
}

func (b *resultsBox) MoveSelectionDownOne() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.selected >= len(b.matches)-1 {
		return
	}
	b.selected++
}

func (b *resultsBox) MoveSelectionUpOne() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.selected <= 0 {
		return
	}
	b.selected--
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

func (b *searchBox) MouseClick(x, y int) {
	return
}

func (b *searchBox) InsertRune(r rune) {

	tail := append([]rune{r}, b.value[b.cursorOffsetX:]...)
	b.value = append(b.value[:b.cursorOffsetX], tail...)
	b.cursorOffsetX++

	go func() {
		results.Recalculate()
		results.SelectBestMatch()
	}()
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

	go func() {
		results.Recalculate()
		results.SelectBestMatch()
	}()
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
