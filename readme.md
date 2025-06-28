# LevelDB Viewer (Terminal UI)

LevelDB Viewer is a graphical terminal-based tool for exploring LevelDB databases. It offers an intuitive interface to view keys and values, filter by key prefixes, search for specific keys, and navigate large datasets with pagination.

![App Screenshot](https://i.imgur.com/CrcmXuB.png)

## Features

- **Graphical UI**: Browse databases using a `tview`-powered terminal interface
- **Key-Value Viewing**: Inspect all keys and values in the database
- **Key Navigation**: Use arrow keys to select keys and view values
- **Data Export**: `d`: Dump current key/value to file; `a`: Export all keys/values to single file
- **Fuzzy Search**: Find keys containing numbers or text patterns

## Installation

Install Go:
```
winget install --id=GoLang.Go -e
```
Clone and build locally:
```
git clone https://github.com/solessfir/leveldb-viewer.git
cd leveldb-viewer
go build
```

Or using `go install`:

```
go install github.com/solessfir/leveldb-viewer@latest
```

## Usage

Launch the tool with your database path:

```
./leveldb-viewer.exe -db /path/to/your/db
```

## Contributing

Contributions are welcome! Open an issue for bugs or features, or submit a pull request.

## License

Licensed under the MIT License. See [LICENSE](LICENSE) for details.

## Acknowledgments

Thanks to [goleveldb](https://github.com/syndtr/goleveldb) and [tview](https://github.com/rivo/tview) for enabling this tool.