package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/iterator"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	pageSize         = 100    // Number of keys per page
	currentPosition  = 0      // Current scroll position in the list
	displayedKeys    [][]byte // Currently displayed keys
	currentPrefix    string   // Current prefix filter
	showHelp         = false  // Show/hide help window
	db               *leveldb.DB
	statusMessage    = ""   // Status bar message
	statusExpiration time.Time
	statusBar        *tview.TextView
	currentMode      = "keys" // "keys" or "value"
	app              *tview.Application
	keyList          *tview.List
	valueView        *tview.TextView
	currentKey       []byte // Track currently selected key
	helpWindow       *tview.TextView
	hasMoreKeys      = true // Indicates if more keys can be loaded
	searchBox        *tview.InputField // Make searchBox global for focus check
)

func main() {
	// Command-line flags
	dbPath := flag.String("db", "", "Path to the LevelDB database")
	flag.Parse()

	// Open the LevelDB database
	var err error
	db, err = leveldb.OpenFile(*dbPath, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Initialize tview application
	app = tview.NewApplication()

	// Create UI components
	keyList = tview.NewList().SetWrapAround(false)
	keyList.SetBorder(true).SetTitle(" Keys ")
	keyList.SetTitleAlign(tview.AlignLeft)
	keyList.SetTitleColor(tcell.ColorYellow)
	keyList.SetMainTextStyle(tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorReset))
	keyList.SetBackgroundColor(tcell.ColorReset)
	keyList.SetSelectedBackgroundColor(tcell.ColorWhite)
	keyList.SetHighlightFullLine(true)

	valueView = tview.NewTextView()
	valueView.SetDynamicColors(true).SetBorder(true).SetTitle(" Value ")
	valueView.SetTitleColor(tcell.ColorYellow)
	valueView.SetTitleAlign(tview.AlignLeft)
	valueView.SetScrollable(true)
	valueView.SetBackgroundColor(tcell.ColorReset)
	valueView.SetTextColor(tcell.ColorWhite)

	statusBar = tview.NewTextView()
	statusBar.SetDynamicColors(true).SetTextAlign(tview.AlignCenter)
	statusBar.SetBackgroundColor(tcell.ColorReset)
	statusBar.SetTextColor(tcell.ColorWhite)
	updateStatusBar()

	// Create search box
	searchBox = tview.NewInputField()
	searchBox.SetLabel(" Search: ")

	// Search label style
	searchBox.SetLabelStyle(tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorReset))

	// Search box field style
	searchBox.SetFieldStyle(tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorReset))
	
	searchBox.SetChangedFunc(func(text string) {
		currentPrefix = text
		loadInitialKeys()
	})

	searchBox.SetDoneFunc(func(key tcell.Key) {
		app.SetFocus(keyList)
	})

	// Esc key support in search box
	searchBox.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc {
			app.SetFocus(keyList)
			return nil
		}
		return event
	})

	helpText := `[::b]KEY SHORTCUTS[::-]
	[white]Arrow Keys[::-]: Navigate keys
	[white]Enter[::-]:       Show selected key's value
	[white]d[::-]:           Dump key/value to file
	[white]a[::-]:           Dump all keys to file
	[white]/[::-]:           Focus search box
	[white]h[::-]:           Toggle help window
	[white]q[::-]:           Quit application

	[::b]IN VALUE VIEW[::-]
	[white]Arrow Keys[::-]: Scroll value content
	[white]Esc[::-]:        Return to key list`

	helpWindow = tview.NewTextView().SetText(helpText)
	helpWindow.SetBorder(true).SetTitle(" Help ")
	helpWindow.SetTitleAlign(tview.AlignCenter)
	helpWindow.SetDynamicColors(true)
	helpWindow.SetBackgroundColor(tcell.ColorReset)
	helpWindow.SetTextColor(tcell.ColorWhite)

	// Layout
	flex := tview.NewFlex().SetDirection(tview.FlexRow)
	flex.AddItem(tview.NewFlex().
		AddItem(keyList, 0, 1, true).
		AddItem(valueView, 0, 2, false), 0, 1, true)
	flex.AddItem(searchBox, 1, 1, false)
	flex.AddItem(statusBar, 1, 1, false)

	// Key handling
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if app.GetFocus() == searchBox {
			// Ignore keys if search box is focused
			return event
		}

		if currentMode == "value" {
			if event.Key() == tcell.KeyEsc {
				app.SetFocus(keyList)
				currentMode = "keys"
				updateStatusBar()
				return nil
			}
			return event
		}

		switch event.Rune() {
		case 'd', 'D':
			dumpCurrentKey()
			return nil
		case 'a', 'A':
			dumpAllKeys()
			return nil
		case 'h', 'H':
			showHelp = !showHelp
			if showHelp {
				flex.AddItem(helpWindow, 0, 1, false)
			} else {
				flex.RemoveItem(helpWindow)
			}
		case '/':
			app.SetFocus(searchBox)
			return nil
		case 'q', 'Q':
			app.Stop()
		}

		switch event.Key() {
		case tcell.KeyEnter:
			showSelectedKeyValue()
			return nil
		case tcell.KeyDown:
			handleScroll(event)
		}

		return event
	})

	// Value view input capture
	valueView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyDown:
			valueView.ScrollToEnd()
		case tcell.KeyUp:
			valueView.ScrollToBeginning()
		}
		return event
	})

	// Show value on key selection change
	keyList.SetChangedFunc(func(index int, mainText string, secondaryText string, shortcut rune) {
		if index >= 0 && index < len(displayedKeys) {
			currentKey = displayedKeys[index]
			showKeyValue(currentKey)
			updateKeyListTitle()
		}
	})

	loadInitialKeys()

	// Start application
	if err := app.SetRoot(flex, true).SetFocus(keyList).Run(); err != nil {
    	log.Fatal(err)
	}
}

func updateStatusBar() {
	if currentMode == "value" {
		statusBar.SetText("[white]Value View[::-] | [white]↑/↓[::-]: Scroll | [white]Esc[::-]: Back to keys")
	} else {
		statusBar.SetText("[white]↑/↓[::-]: Navigate | [white]Enter[::-]: Focus Value | [white]d[::-]: Dump Key | [white]a[::-]: Dump All | [white]/[::-]: Search | [white]h[::-]: Help | [white]q[::-]: Quit")
	}
}

func showSelectedKeyValue() {
	currentIndex := keyList.GetCurrentItem()
	if currentIndex >= 0 && currentIndex < len(displayedKeys) {
		currentKey = displayedKeys[currentIndex]
		app.SetFocus(valueView)
		currentMode = "value"
		updateStatusBar()
	}
}

// Load the initial page of keys based on the current prefix
func loadInitialKeys() {
	keyList.Clear()
	currentPosition = 0
	displayedKeys = [][]byte{}
	hasMoreKeys = true

	iter := db.NewIterator(nil, nil)
	defer iter.Release()

	// Convert search term to lowercase once
	searchLower := strings.ToLower(currentPrefix)
	
	for iter.Next() {
		key := iter.Key()
		keyStr := string(key)
		
		// Case-insensitive substring search
		if currentPrefix == "" || strings.Contains(strings.ToLower(keyStr), searchLower) {
			keyCopy := append([]byte{}, key...)
			displayedKeys = append(displayedKeys, keyCopy)
			keyList.AddItem(keyStr, "", 0, nil)
			
			// Stop when we have a full page
			if len(displayedKeys) >= pageSize {
				break
			}
		}
	}
	
	// Check if there are more keys
	hasMoreKeys = iter.Next()
	if err := iter.Error(); err != nil {
		setStatus(fmt.Sprintf("[red]Error: %v", err))
	}
	
	updateKeyListTitle()
}

// Load a page of keys from the iterator
func loadPage(iter iterator.Iterator) {
	searchLower := strings.ToLower(currentPrefix)
	
	for i := 0; i < pageSize && iter.Next(); {
		key := iter.Key()
		keyStr := string(key)
		
		// Case-insensitive substring search
		if currentPrefix == "" || strings.Contains(strings.ToLower(keyStr), searchLower) {
			keyCopy := append([]byte{}, key...)
			displayedKeys = append(displayedKeys, keyCopy)
			keyList.AddItem(keyStr, "", 0, nil)
			i++
		}
	}
	hasMoreKeys = iter.Next() // Check if there are more keys
	if err := iter.Error(); err != nil {
		setStatus(fmt.Sprintf("[red]Error: %v", err))
	}
}

// Load the next page of keys when scrolling down
func loadNextPage() bool {
	if !hasMoreKeys || len(displayedKeys) == 0 {
		return false
	}

	// Start from the last key we loaded
	lastKey := displayedKeys[len(displayedKeys)-1]
	iter := db.NewIterator(nil, nil)
	defer iter.Release()

	searchLower := strings.ToLower(currentPrefix)
	
	// Seek to the last loaded key
	if iter.Seek(lastKey) {
		iter.Next()
		
		// Load next page of keys
		count := 0
		for ; iter.Valid() && count < pageSize; iter.Next() {
			key := iter.Key()
			keyStr := string(key)
			
			// Case-insensitive substring search
			if currentPrefix == "" || strings.Contains(strings.ToLower(keyStr), searchLower) {
				keyCopy := append([]byte{}, key...)
				displayedKeys = append(displayedKeys, keyCopy)
				keyList.AddItem(keyStr, "", 0, nil)
				count++
			}
		}
		hasMoreKeys = iter.Valid()
		return count > 0
	}
	return false
}

// Handle scroll events to load more keys
func handleScroll(event *tcell.EventKey) {
	currentIndex := keyList.GetCurrentItem()
	itemCount := keyList.GetItemCount()

	if event.Key() == tcell.KeyDown && currentIndex == itemCount-1 && hasMoreKeys {
		if loadNextPage() {
			keyList.SetCurrentItem(currentIndex + 1)
			setStatus(fmt.Sprintf("[green]Loaded %d keys total", len(displayedKeys)))
			updateKeyListTitle()
		}
	} else if event.Key() == tcell.KeyDown || event.Key() == tcell.KeyUp {
		updateKeyListTitle()
	}
}

// Show key value in detail view
func showKeyValue(key []byte) {
	value, err := db.Get(key, nil)
	if err != nil {
		valueView.SetText(fmt.Sprintf("[red]Error: %v", err))
		return
	}
	
	if len(value) == 0 {
		valueView.SetText(fmt.Sprintf("[white]Key[::-]: %s\n\n[white]Value[::-]: (empty)", key))
		return
	}
	
	displayStr := formatValue(value)
	valueView.SetText(fmt.Sprintf("[white]Key[::-]: %s\n\n[white]Value[::-]: %s", key, displayStr))
}

func formatValue(value []byte) string {
	if json.Valid(value) {
		var prettyJSON bytes.Buffer
		if err := json.Indent(&prettyJSON, value, "", "  "); err == nil {
			return prettyJSON.String()
		}
	}
	return mixedContentDisplay(value)
}

// Dump current key to file
func dumpCurrentKey() {
	currentIndex := keyList.GetCurrentItem()
	if currentIndex < 0 || currentIndex >= len(displayedKeys) {
		setStatus("[red]Invalid selection")
		return
	}

	key := displayedKeys[currentIndex]
	value, err := db.Get(key, nil)
	if err != nil {
		setStatus(fmt.Sprintf("[red]Error: %v", err))
		return
	}

	dir := "leveldb_dump"
	if err := os.MkdirAll(dir, 0755); err != nil {
		setStatus(fmt.Sprintf("[red]Error creating directory: %v", err))
		return
	}

	filename := strings.Map(func(r rune) rune {
		if r < 32 || r == '/' || r == '\\' || r == ':' || r == '*' ||
			r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return '_'
		}
		return r
	}, string(key))

	filePath := filepath.Join(dir, filename+".txt")
	
	// Format value the same way it's displayed in UI
	formattedValue := formatValue(value)
	content := fmt.Sprintf("Key: %s\n\nValue: %s", key, formattedValue)
	
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		setStatus(fmt.Sprintf("[red]Error writing file: %v", err))
		return
	}

	setStatus(fmt.Sprintf("[green]Dumped to %s", filePath))
}

func dumpAllKeys() {
	dir := "leveldb_dump"
	if err := os.MkdirAll(dir, 0755); err != nil {
		setStatus(fmt.Sprintf("[red]Error creating directory: %v", err))
		return
	}

	filePath := filepath.Join(dir, "all_keys.txt")
	file, err := os.Create(filePath)
	if err != nil {
		setStatus(fmt.Sprintf("[red]Error creating file: %v", err))
		return
	}
	defer file.Close()

	iter := db.NewIterator(nil, nil)
	defer iter.Release()

	count := 0
	for iter.Next() {
		key := iter.Key()
		value := iter.Value()
		formattedValue := formatValue(value)
		content := fmt.Sprintf("Key: %s\n\nValue: %s\n\n%s\n", key, formattedValue, strings.Repeat("-", 80))
		
		if _, err := file.WriteString(content); err != nil {
			setStatus(fmt.Sprintf("[red]Error writing key: %v", err))
			return
		}

		count++
	}

	if err := iter.Error(); err != nil {
		setStatus(fmt.Sprintf("[red]Iterator error: %v", err))
		return
	}

	setStatus(fmt.Sprintf("[green]Dumped %d keys to %s", count, filePath))
}

func mixedContentDisplay(value []byte) string {
	var result strings.Builder
	pos := 0
	var binaryBuffer []byte

	flushBinary := func() {
		if len(binaryBuffer) > 0 {
			result.WriteString("[b64:")
			result.WriteString(base64.RawStdEncoding.EncodeToString(binaryBuffer))
			result.WriteString("]")
			binaryBuffer = nil
		}
	}

	for pos < len(value) {
		r, size := utf8.DecodeRune(value[pos:])
		if r == utf8.RuneError && size == 1 {
			// Invalid UTF-8 byte
			binaryBuffer = append(binaryBuffer, value[pos])
			pos++
		} else if unicode.IsControl(r) {
			// Control character, collect its bytes
			binaryBuffer = append(binaryBuffer, value[pos:pos+size]...)
			pos += size
		} else {
			// Flush any pending binary data
			flushBinary()
			
			// Write printable rune
			result.WriteRune(r)
			pos += size
		}
	}
	
	// Flush any remaining binary data
	flushBinary()
	
	return result.String()
}

// Set status message with expiration
func setStatus(message string) {
	statusMessage = message
	statusExpiration = time.Now().Add(5 * time.Second)
	statusBar.SetText(message)

	go func() {
		time.Sleep(5 * time.Second)
		if time.Now().After(statusExpiration) {
			updateStatusBar()
		}
	}()
}

// Update the Keys title with current position
func updateKeyListTitle() {
	if len(displayedKeys) == 0 {
		keyList.SetTitle(" Keys ")
	} else {
		currentIndex := keyList.GetCurrentItem()
		keyList.SetTitle(fmt.Sprintf(" Keys (%d/%d) ", currentIndex+1, len(displayedKeys)))
	}
}