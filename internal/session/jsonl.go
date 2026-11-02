package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/klauspost/compress/zstd"
)

// zstdSuffix names a Zstandard-compressed JSONL transcript. Its decompressed
// stream is read in place; nothing is written beside the original.
const zstdSuffix = ".jsonl.zst"

func isZstdTranscript(path string) bool {
	return strings.HasSuffix(path, zstdSuffix)
}

// forEachLine streams a transcript file line by line, calling visit with
// 1-based line numbers until it returns false or the file ends. Lines can be
// arbitrarily long; the trailing newline is trimmed. A Zstandard-compressed
// transcript is decompressed while streaming, so line numbers address the
// decompressed content and memory stays bounded by the frame's window.
func forEachLine(path string, visit func(line int, data []byte) bool) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return err
	}

	var input io.Reader = file
	compressed := isZstdTranscript(path)
	if compressed {
		decoder, err := zstd.NewReader(file, zstd.WithDecoderConcurrency(1), zstd.WithDecoderLowmem(true))
		if err != nil {
			return fmt.Errorf("decompress transcript: %w", err)
		}
		defer decoder.Close()
		input = decoder
	}
	if err := readLines(input, visit); err != nil {
		if compressed {
			return fmt.Errorf("decompress transcript: %w", err)
		}
		return err
	}
	current, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("verify transcript after reading; rerun the command: %w", err)
	}
	if !os.SameFile(info, current) {
		return fmt.Errorf("transcript %q was replaced while reading; rerun the command", path)
	}
	return nil
}

func readLines(input io.Reader, visit func(int, []byte) bool) error {
	reader := bufio.NewReaderSize(input, 64*1024)
	line := 0
	for {
		data, err := reader.ReadBytes('\n')
		if len(data) > 0 {
			line++
			if !visit(line, bytes.TrimRight(data, "\r\n")) {
				return nil
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

type fileIdentity struct {
	device uint64
	inode  uint64
}

// walkRules parameterizes JSONL transcript discovery for one harness grammar.
type walkRules struct {
	// accept reports whether a JSONL file is a session transcript.
	accept func(path string) bool
	// isSubagent reports whether a transcript belongs to a subagent.
	isSubagent func(path string) bool
	// skipDir reports whether a directory subtree holds no wanted transcripts.
	skipDir func(path string, entry fs.DirEntry) bool
	// zstd reports whether Zstandard-compressed .jsonl.zst files are transcripts too.
	zstd bool
}

func (r walkRules) isTranscript(path string) bool {
	return filepath.Ext(path) == ".jsonl" || (r.zstd && isZstdTranscript(path))
}

// walkJSONLTranscripts discovers transcript files under root, tolerating and
// recording per-entry failures instead of aborting.
func walkJSONLTranscripts(root string, rules walkRules) ([]transcript, []Diagnostic) {
	var transcripts []transcript
	var diagnostics []Diagnostic
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Code: codeWalk, Message: err.Error(), Path: path})
			return nil
		}
		if entry.IsDir() {
			if path != root && rules.skipDir != nil && rules.skipDir(path, entry) {
				return fs.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() || !rules.isTranscript(path) {
			return nil
		}
		if rules.accept != nil && !rules.accept(path) {
			return nil
		}
		subagent := rules.isSubagent != nil && rules.isSubagent(path)
		info, infoErr := entry.Info()
		if infoErr != nil {
			diagnostics = append(diagnostics, Diagnostic{Code: codeWalk, Message: infoErr.Error(), Path: path})
			return nil
		}
		stat := info.Sys().(*syscall.Stat_t)
		transcripts = append(transcripts, transcript{
			identity: fileIdentity{device: uint64(stat.Dev), inode: stat.Ino},
			modTime:  info.ModTime(), path: path, subagent: subagent,
		})
		return nil
	})
	return transcripts, diagnostics
}

// flattenText extracts human-readable text from a value that is either a
// JSON string or an array of content blocks carrying "text" fields.
func flattenText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// compactJSON renders raw JSON on one line for matching and excerpts.
func compactJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var b bytes.Buffer
	if json.Compact(&b, raw) != nil {
		return string(raw)
	}
	return b.String()
}

// parseTimestamp reads an RFC 3339 timestamp, returning zero on failure.
func parseTimestamp(value string) time.Time {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return t
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
