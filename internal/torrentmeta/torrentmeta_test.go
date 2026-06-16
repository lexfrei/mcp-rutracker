package torrentmeta_test

import (
	"crypto/sha1"
	"fmt"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"

	"github.com/lexfrei/mcp-rutracker/internal/torrentmeta"
)

const infoSingle = `d6:lengthi1024e4:name8:file.txt12:piece lengthi16384ee`

const infoMulti = `d5:filesl` +
	`d6:lengthi100e4:pathl1:a5:b.txtee` +
	`d6:lengthi200e4:pathl5:c.mp4ee` +
	`e4:name3:dir12:piece lengthi16384ee`

const fileName = "file.txt"

// expectedHash recomputes the canonical info-hash for a raw info dictionary.
func expectedHash(info string) string {
	sum := sha1.Sum([]byte(info))

	return strings.ToUpper(fmt.Sprintf("%x", sum))
}

func TestParse_SingleFile(t *testing.T) {
	t.Parallel()

	torrent := []byte(`d4:info` + infoSingle + `e`)

	meta, err := torrentmeta.Parse(torrent)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if meta.Name != fileName {
		t.Errorf("Name = %q, want file.txt", meta.Name)
	}

	if meta.FileCount != 1 || len(meta.Files) != 1 {
		t.Fatalf("FileCount = %d, want 1", meta.FileCount)
	}

	if meta.Files[0].Path != fileName || meta.Files[0].SizeBytes != 1024 {
		t.Errorf("file = %+v, want {file.txt 1024}", meta.Files[0])
	}

	if meta.TotalSizeBytes != 1024 {
		t.Errorf("TotalSizeBytes = %d, want 1024", meta.TotalSizeBytes)
	}

	if meta.PieceLength != 16384 {
		t.Errorf("PieceLength = %d, want 16384", meta.PieceLength)
	}

	if meta.InfoHash != expectedHash(infoSingle) {
		t.Errorf("InfoHash = %q, want %q", meta.InfoHash, expectedHash(infoSingle))
	}
}

func TestParse_MultiFile(t *testing.T) {
	t.Parallel()

	torrent := []byte(`d4:info` + infoMulti + `e`)

	meta, err := torrentmeta.Parse(torrent)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if meta.Name != "dir" {
		t.Errorf("Name = %q, want dir", meta.Name)
	}

	if meta.FileCount != 2 {
		t.Fatalf("FileCount = %d, want 2", meta.FileCount)
	}

	wantPaths := []string{"dir/a/b.txt", "dir/c.mp4"}
	wantSizes := []int64{100, 200}

	for i, file := range meta.Files {
		if file.Path != wantPaths[i] || file.SizeBytes != wantSizes[i] {
			t.Errorf("file[%d] = %+v, want {%s %d}", i, file, wantPaths[i], wantSizes[i])
		}
	}

	if meta.TotalSizeBytes != 300 {
		t.Errorf("TotalSizeBytes = %d, want 300", meta.TotalSizeBytes)
	}

	if meta.InfoHash != expectedHash(infoMulti) {
		t.Errorf("InfoHash = %q, want %q", meta.InfoHash, expectedHash(infoMulti))
	}
}

func TestParse_RejectsEmptyNameAndFiles(t *testing.T) {
	t.Parallel()

	cases := map[string][]byte{
		"missing name":     []byte(`d4:infod6:lengthi10eee`),
		"empty files list": []byte(`d4:infod5:filesle4:name3:diree`),
	}

	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := torrentmeta.Parse(data)
			if !errors.Is(err, torrentmeta.ErrInvalidTorrent) {
				t.Fatalf("expected ErrInvalidTorrent, got %v", err)
			}
		})
	}
}

func TestParse_SingleFileMissingLength(t *testing.T) {
	t.Parallel()

	// A single-file torrent with a name but no length is accepted leniently:
	// one entry with size 0, so download enrichment still returns a hash.
	meta, err := torrentmeta.Parse([]byte(`d4:infod4:name8:file.txtee`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if meta.FileCount != 1 || meta.Files[0].Path != fileName || meta.Files[0].SizeBytes != 0 {
		t.Errorf("files = %+v, want one entry file.txt (0 bytes)", meta.Files)
	}
}

func TestParse_SkipsMalformedFileEntries(t *testing.T) {
	t.Parallel()

	// A files list with one integer (malformed) and one valid entry: the bad
	// entry is skipped, the good one survives.
	torrent := []byte(`d4:info` +
		`d5:files` +
		`l` +
		`i99e` + // malformed: an integer where a file dict is expected
		`d6:lengthi500e4:pathl6:a.flacee` + // valid: dir/a.flac, 500 bytes
		`e` + // close files list
		`4:name3:dir12:piece lengthi16384e` +
		`e` + // close info dict
		`e`) // close top dict

	meta, err := torrentmeta.Parse(torrent)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if meta.FileCount != 1 || meta.Files[0].Path != "dir/a.flac" || meta.Files[0].SizeBytes != 500 {
		t.Errorf("files = %+v, want one entry dir/a.flac (500)", meta.Files)
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte(`d4:info` + infoSingle + `e`))
	f.Add([]byte(`d4:info` + infoMulti + `e`))
	f.Add([]byte("d"))
	f.Add([]byte(""))
	f.Add([]byte("llll"))

	f.Fuzz(func(_ *testing.T, data []byte) {
		// Parse must never panic on arbitrary input; it may only return a value
		// or an error.
		_, _ = torrentmeta.Parse(data)
	})
}

func TestParse_DeepNestingRejected(t *testing.T) {
	t.Parallel()

	// Deeply nested input must be rejected with an error rather than recursing
	// until the goroutine stack overflows (a fatal, unrecoverable crash).
	cases := map[string][]byte{
		"nested lists": []byte(strings.Repeat("l", 2000)),
		"nested dicts": []byte("d4:info" + strings.Repeat("d1:x", 2000)),
	}

	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := torrentmeta.Parse(data)
			if !errors.Is(err, torrentmeta.ErrInvalidTorrent) {
				t.Fatalf("expected ErrInvalidTorrent, got %v", err)
			}
		})
	}
}

func TestParse_Invalid(t *testing.T) {
	t.Parallel()

	cases := map[string][]byte{
		"empty":         {},
		"not a dict":    []byte("i123e"),
		"missing info":  []byte("d8:announce3:abce"),
		"truncated":     []byte("d4:info"),
		"bad integer":   []byte("d4:infod6:lengthixxeee"),
		"short string":  []byte("d4:info99:tooshorte"),
		"trailing data": []byte(`d4:info` + infoSingle + `eEXTRA`),
		// A length near math.MaxInt must be rejected, not overflow into a
		// negative slice bound and panic.
		"overflow length": []byte(`d4:infod4:name9223372036854775807:xee`),
	}

	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := torrentmeta.Parse(data)
			if !errors.Is(err, torrentmeta.ErrInvalidTorrent) {
				t.Fatalf("expected ErrInvalidTorrent, got %v", err)
			}
		})
	}
}
