package main

import (
	"github.com/nsf/termbox-go"
)

var search = searchBox{
	cursorOffsetX: 0,
	cursorOffsetY: 0,
	value:         []rune{},
}

func main() {
	err := termbox.Init()
	if err != nil {
		panic(err)
	}
	defer termbox.Close()
	termbox.SetInputMode(termbox.InputAlt)

	redrawAll(nil)
	for {
		switch ev := termbox.PollEvent(); ev.Type {
		case termbox.EventKey:
			switch ev.Key {
			case termbox.KeyEsc:
				return
			case termbox.KeyCtrlC:
				return
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
			// 	edit_box.DeleteRuneForward()
			case termbox.KeyTab:
			// 	edit_box.InsertRune('\t')
			case termbox.KeySpace:
			// 	edit_box.InsertRune(' ')
			case termbox.KeyCtrlK:
			// 	edit_box.DeleteTheRestOfTheLine()
			case termbox.KeyHome, termbox.KeyCtrlA:
			// 	edit_box.MoveCursorToBeginningOfTheLine()
			case termbox.KeyEnd, termbox.KeyCtrlE:
			// 	edit_box.MoveCursorToEndOfTheLine()
			default:
				if ev.Ch != 0 {
					search.InsertRune(ev.Ch)
				}
			}
		case termbox.EventError:
			panic(ev.Err)
		}
		redrawAll(nil)
	}
}

type searchBox struct {
	cursorOffsetX int
	cursorOffsetY int
	value         []rune
}

func (b *searchBox) Draw(ev *termbox.Event) {
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
	b.value = append(b.value[0:b.cursorOffsetX], tail...)
	b.cursorOffsetX++
}

func (b *searchBox) MoveCursorOneRuneBackward() {
	if b.cursorOffsetX > 0 {
		b.cursorOffsetX--
	}
}

func (b *searchBox) MoveCursorOneRuneForward() {
	if b.cursorOffsetX < len(b.value) {
		b.cursorOffsetX++
	}
}

func (b *searchBox) DeleteRuneBackward() {
	if b.cursorOffsetX > 0 {
		b.value = b.value[:len(b.value)-1]
		b.cursorOffsetX--
	}
}

func (b *searchBox) DeleteWordBackward() {
	b.value = []rune{}
	b.cursorOffsetX = 0
}

func redrawAll(ev *termbox.Event) {
	termbox.Clear(termbox.ColorDefault, termbox.ColorDefault)
	search.Draw(ev)
	termbox.Flush()
}
