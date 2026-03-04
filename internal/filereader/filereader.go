package filereader

import (
	"bytes"
	"io"
	"os"
)

const defaultTailBytes = 8192

// TailLines reads the last nbytes of a file and returns the lines.
// It skips any partial first line after seeking. If nbytes <= 0, defaults to 8192.
func TailLines(path string, nbytes int64) ([]string, error) {
	if nbytes <= 0 {
		nbytes = defaultTailBytes
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	size := info.Size()
	if size > nbytes {
		if _, err := f.Seek(-nbytes, io.SeekEnd); err != nil {
			return nil, err
		}
		// Skip partial first line
		buf := make([]byte, 1)
		for {
			_, err := f.Read(buf)
			if err != nil {
				break
			}
			if buf[0] == '\n' {
				break
			}
		}
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}

	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}

	lines := bytes.Split(data, []byte("\n"))
	result := make([]string, 0, len(lines))
	for _, l := range lines {
		s := string(l)
		if s != "" {
			result = append(result, s)
		}
	}
	return result, nil
}

// TailBytes reads the last nbytes of a file and returns the raw data.
// It skips any partial first line after seeking.
func TailBytes(path string, nbytes int64) ([]byte, error) {
	if nbytes <= 0 {
		nbytes = defaultTailBytes
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	size := info.Size()
	if size > nbytes {
		if _, err := f.Seek(-nbytes, io.SeekEnd); err != nil {
			return nil, err
		}
		// Skip partial first line
		buf := make([]byte, 1)
		for {
			_, err := f.Read(buf)
			if err != nil {
				break
			}
			if buf[0] == '\n' {
				break
			}
		}
	}

	return io.ReadAll(f)
}
