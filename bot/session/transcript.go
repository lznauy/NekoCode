package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"nekocode/bot/provider/types"
)

const transcriptFileName = "transcript.jsonl"

var syncTranscriptDir = syncDir

type transcriptRecord struct {
	Seq     int           `json:"seq"`
	Message types.Message `json:"message"`
}

func loadTranscript(sessionDir string, confirmed int) ([]types.Message, error) {
	path := filepath.Join(sessionDir, transcriptFileName)
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var messages []types.Message
	// good tracks the byte offset just past the last accepted record. Anything
	// beyond it was never acknowledged by the session.json watermark and can be
	// discarded as a crash-truncated tail.
	var good int64
	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 16<<20)
	for scanner.Scan() {
		raw := scanner.Bytes()
		var record transcriptRecord
		if err := json.Unmarshal(raw, &record); err != nil || record.Seq != len(messages)+1 {
			if len(messages) < confirmed {
				if err != nil {
					return nil, fmt.Errorf("decode confirmed transcript record %d: %w", len(messages)+1, err)
				}
				return nil, fmt.Errorf("confirmed transcript sequence %d, want %d", record.Seq, len(messages)+1)
			}
			// A torn final record is the crash window between append and fsync:
			// the watermark never confirmed it, so drop it instead of bricking
			// the session. Corruption before the last line stays fatal.
			if more := scanner.Scan(); more {
				if err != nil {
					return nil, fmt.Errorf("decode transcript record %d: %w", len(messages)+1, err)
				}
				return nil, fmt.Errorf("transcript sequence %d, want %d", record.Seq, len(messages)+1)
			}
			break
		}
		messages = append(messages, record.Message)
		good += int64(len(raw)) + 1
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read transcript: %w", err)
	}
	if info, err := file.Stat(); err != nil {
		return nil, err
	} else if size := info.Size(); size != good {
		// The accepted records end at `good`. Anything beyond was never
		// acknowledged by the session.json watermark and is dropped as a
		// crash-truncated tail. A size of good-1 means the final record's
		// newline never reached the disk: repair by appending it, or the next
		// O_APPEND write would concatenate onto the same line.
		switch size {
		case good - 1:
			if err := appendNewline(path); err != nil {
				return nil, fmt.Errorf("repair transcript newline: %w", err)
			}
		default:
			if size < good-1 {
				return nil, fmt.Errorf("transcript shrank: %d bytes for %d confirmed", size, good)
			}
			if err := os.Truncate(path, good); err != nil {
				return nil, fmt.Errorf("truncate unconfirmed transcript tail: %w", err)
			}
		}
	}
	return messages, nil
}

func appendNewline(path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, err = file.Write([]byte{'\n'})
	if syncErr := file.Sync(); err == nil {
		err = syncErr
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}

func appendTranscript(sessionDir string, persisted int, messages []types.Message) (int, error) {
	if persisted < 0 || persisted > len(messages) {
		return persisted, fmt.Errorf("invalid transcript watermark %d for %d messages", persisted, len(messages))
	}
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		return persisted, err
	}
	transcriptPath := filepath.Join(sessionDir, transcriptFileName)
	if _, err := os.Stat(transcriptPath); os.IsNotExist(err) {
		// load() and save() keep the watermark and the file in lockstep, so a
		// missing file with a non-zero watermark means the file was lost or
		// removed externally. Rewriting from scratch would duplicate records.
		if persisted > 0 {
			return persisted, fmt.Errorf("transcript file missing but %d records were persisted", persisted)
		}
	} else if err != nil {
		return persisted, err
	}
	if persisted == len(messages) {
		return persisted, nil
	}
	file, err := os.OpenFile(transcriptPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return persisted, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return persisted, err
	}
	originalSize := info.Size()
	rollback := func(cause error) (int, error) {
		if truncateErr := file.Truncate(originalSize); truncateErr == nil {
			_ = file.Sync()
		} else {
			cause = fmt.Errorf("%w; transcript rollback failed: %v", cause, truncateErr)
		}
		_ = file.Close()
		return persisted, cause
	}
	encoder := json.NewEncoder(file)
	for i := persisted; i < len(messages); i++ {
		if err := encoder.Encode(transcriptRecord{Seq: i + 1, Message: messages[i]}); err != nil {
			return rollback(fmt.Errorf("append transcript record %d: %w", i+1, err))
		}
	}
	if err := file.Sync(); err != nil {
		return rollback(fmt.Errorf("sync transcript: %w", err))
	}
	if err := file.Close(); err != nil {
		return len(messages), err
	}
	if err := syncTranscriptDir(sessionDir); err != nil {
		return len(messages), err
	}
	return len(messages), nil
}
