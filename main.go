package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"

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
	EventMousePress
	EventMouseClick
	EventMouseScrollDown
	EventMouseScrollUp

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

func initBasepath() (basepath string) {
	defer func() {
		_, err := os.Stat(basepath)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Println("no such file or directory")
				os.Exit(1)
			}
			panic(err)
		}
	}()

	if len(os.Args) > 1 {
		path, err := filepath.Abs(os.Args[1])
		if err != nil {
			panic(err)
		}
		return path
	}
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	path := wd
	for ; path != "/"; path = filepath.Dir(path) {
		// keep walking up the tree until we find a git root
		info, err := os.Stat(filepath.Join(path, ".git"))
		if err == nil && info.IsDir() {
			break
		}
	}
	if path == "/" {
		path = wd
	}
	return path
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
	var prev event
	for {
		func() {
			ev := termbox.PollEvent()
			if ev.Type == termbox.EventError {
				eventCh <- event{evType: EventError, err: ev.Err}
				return
			}

			// Mouse events
			if ev.Type == termbox.EventMouse {
				var curr event
				switch ev.Key {
				case termbox.MouseRelease:
					if prev.evType == EventMousePress {
						curr = event{evType: EventMouseClick, mouseX: ev.MouseX, mouseY: ev.MouseY}
					}
				case termbox.MouseWheelDown:
					curr = event{evType: EventMouseScrollDown, mouseX: ev.MouseX, mouseY: ev.MouseY}
				case termbox.MouseWheelUp:
					curr = event{evType: EventMouseScrollUp, mouseX: ev.MouseX, mouseY: ev.MouseY}
				case termbox.MouseLeft:
					if prev.evType == EventMousePress || prev.evType == EventMouseDrag {
						curr = event{evType: EventMouseDrag, mouseX: ev.MouseX, mouseY: ev.MouseY}
					} else {
						curr = event{evType: EventMousePress, mouseX: ev.MouseX, mouseY: ev.MouseY}
					}
				default:
				}
				prev = curr
				// skipping mouse events keeps the UI speedy
				select {
				case eventCh <- curr:
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

	draw()
	for ev := range eventCh {
		switch ev.evType {
		case EventSelected:
			return results.Selected(), nil
		case EventShutdown:
			return ".", nil // TODO: os.Exit?
		case EventError:
			return ".", ev.err
		}

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
		case EventMouseDrag, EventMousePress:
			results.MousePress(ev.mouseY)
		case EventMouseScrollDown:
			results.MouseScrollDown()
		case EventMouseScrollUp:
			results.MouseScrollUp()
		case EventMouseClick:
			results.MouseClick(ev.mouseX, ev.mouseY, eventCh)
		}
		draw()
	}

	return ".", nil
}

var drawMutex sync.Mutex

func draw() {
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

type resultsBox struct {
	matches        []string
	selected       int
	displayOffsetY int

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
		filename, err := filepath.Abs(filepath.Join(dirname, info.Name()))
		if err != nil {
			panic(err)
		}
		if info.IsDir() {
			dirpaths = append(dirpaths, filename)
			go readirs(filename, filepaths)
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
		draw()
	}
}

func (b *resultsBox) Draw() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i := b.displayOffsetY; i < len(b.matches); i++ {
		y := i - b.displayOffsetY
		path := b.matches[i]
		fg, bg := termbox.ColorDefault, termbox.ColorDefault
		if y+b.displayOffsetY == b.selected {
			termbox.SetCell(0, y+3, '►', fg, bg)
			fg = termbox.AttrBold | termbox.AttrUnderline
		}
		for x, r := range search.displayPath(path) {
			termbox.SetCell(x+2, y+3, r, fg, bg)
		}
	}
}

func (b *resultsBox) focusTop() {
	b.displayOffsetY = b.selected
}

func (b *resultsBox) focusBottom() {
	_, h := termbox.Size()
	b.displayOffsetY = b.selected - (h - 3) + 1
}

func (b *resultsBox) MousePress(y int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if y-3 < 0 {
		go b.MouseScrollUp()
		return
	}

	if y-3 >= len(b.matches) {
		go b.MouseScrollDown()
		return
	}

	b.selected = y + b.displayOffsetY - 3
}

func (b *resultsBox) MouseClick(x, y int, eventCh chan<- event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if y+b.displayOffsetY-3 != b.selected {
		return
	}
	if x-2 < 0 || x-2 >= len([]rune(search.displayPath(b.matches[b.selected]))) {
		return
	}
	go func() {
		eventCh <- event{evType: EventSelected}
	}()
}

func (b *resultsBox) MouseScrollDown() {
	b.mu.Lock()
	defer b.mu.Unlock()

	_, h := termbox.Size()

	if h-3 > len(b.matches)-b.displayOffsetY {
		return
	}

	b.displayOffsetY++
}

func (b *resultsBox) MouseScrollUp() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.displayOffsetY <= 0 {
		return
	}

	b.displayOffsetY--
}

func (b *resultsBox) MoveSelectionDownOne() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.selected < len(b.matches)-1 {
		b.selected++
	}

	// selected is off screen up above
	if b.selected < b.displayOffsetY {
		b.focusTop()
	}

	// selected is off screen down below
	_, h := termbox.Size()
	if b.displayOffsetY+(h-4) < b.selected {
		b.focusBottom()
	}
}

func (b *resultsBox) MoveSelectionUpOne() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.selected > 0 {
		b.selected--
	}

	// selected is off screen up above
	if b.selected < b.displayOffsetY {
		b.focusTop()
	}

	// selected is off screen down below
	_, h := termbox.Size()
	if b.displayOffsetY+(h-4) < b.selected {
		b.focusBottom()
	}
}

func (b *resultsBox) AppendFilepaths(filepaths []string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	a := b
	_ = a

	all := append(b.filepaths, filepaths...)
	sort.Slice(all, func(i, j int) bool {
		si := search.Score(all[i])
		sj := search.Score(all[j])
		if si == sj {
			if len(all[i]) == len(all[j]) {
				return all[i] < all[j]
			}
			return len(all[i]) < len(all[j])
		}
		return si > sj
	})
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

	var bestScore float32
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

func (b *searchBox) Score(path string) float32 {
	// everything matches an empty query equally
	if len(b.value) == 0 {
		return 1
	}
	partial := b.displayPath(path)
	var score float32 = 1
	var i int
	for _, q := range b.value {
		partial = strings.ToLower(partial[i:])
		i = strings.IndexRune(partial, unicode.ToLower(q))
		if i < 0 {
			return 0
		}
		i++
		score += float32(i)
	}
	return 1 / score
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
	b.mu.Lock()
	defer b.mu.Unlock()

	tail := append([]rune{r}, b.value[b.cursorOffsetX:]...)
	b.value = append(b.value[:b.cursorOffsetX], tail...)
	b.cursorOffsetX++

	go func() {
		results.Recalculate()
		results.SelectBestMatch()
	}()
}

func (b *searchBox) MoveCursorOneRuneBackward() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.cursorOffsetX <= 0 {
		return
	}
	b.cursorOffsetX--
}

func (b *searchBox) MoveCursorOneRuneForward() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.cursorOffsetX >= len(b.value) {
		return
	}
	b.cursorOffsetX++
}

func (b *searchBox) MoveCursorOneWordBackward() {
	b.mu.Lock()
	defer b.mu.Unlock()

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
	b.mu.Lock()
	defer b.mu.Unlock()

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
	b.mu.Lock()
	defer b.mu.Unlock()

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
	b.mu.Lock()
	defer b.mu.Unlock()

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
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.cursorOffsetX >= len(b.value) {
		return
	}
	b.value = append(b.value[:b.cursorOffsetX], b.value[b.cursorOffsetX+1:]...)

	go func() {
		results.Recalculate()
		results.SelectBestMatch()
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
