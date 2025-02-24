package detect

import (
	"io"
	"os"
	"strings"

	"github.com/h2non/filetype"
	"github.com/rs/zerolog/log"
	"github.com/zricethezav/gitleaks/v8/report"
	"github.com/zricethezav/gitleaks/v8/sources"
)

func (d *Detector) DetectFiles(paths <-chan sources.ScanTarget) ([]report.Finding, error) {
	for pa := range paths {
		d.Sema.Go(func() error {
			logger := log.With().Str("path", pa.Path).Logger()
			logger.Trace().Msg("Scanning path")

			f, err := os.Open(pa.Path)
			if err != nil {
				if os.IsPermission(err) {
					logger.Warn().Msg("Skipping file: permission denied")
					return nil
				}
				return err
			}
			defer func() {
				_ = f.Close()
			}()

			// Get file size
			fileInfo, err := f.Stat()
			if err != nil {
				return err
			}
			fileSize := fileInfo.Size()
			if d.MaxTargetMegaBytes > 0 {
				rawLength := fileSize / 1000000
				if rawLength > int64(d.MaxTargetMegaBytes) {
					logger.Debug().
						Int64("size", rawLength).
						Msg("Skipping file: exceeds --max-target-megabytes")
					return nil
				}
			}

			// Buffer to hold file chunks
			buf := make([]byte, chunkSize)
			totalLines := 0
			for {
				n, err := f.Read(buf)
				if err != nil && err != io.EOF {
					return err
				}
				if n == 0 {
					break
				}

				// TODO: optimization could be introduced here
				mimetype, err := filetype.Match(buf[:n])
				if err != nil {
					return err
				}
				if mimetype.MIME.Type == "application" {
					return nil // skip binary files
				}

				// Count the number of newlines in this chunk
				linesInChunk := strings.Count(string(buf[:n]), "\n")
				totalLines += linesInChunk
				fragment := Fragment{
					Raw:      string(buf[:n]),
					FilePath: pa.Path,
				}
				if pa.Symlink != "" {
					fragment.SymlinkFile = pa.Symlink
				}
				for _, finding := range d.Detect(fragment) {
					// need to add 1 since line counting starts at 1
					finding.StartLine += (totalLines - linesInChunk) + 1
					finding.EndLine += (totalLines - linesInChunk) + 1
					d.addFinding(finding)
				}
			}

			return nil
		})
	}

	if err := d.Sema.Wait(); err != nil {
		return d.findings, err
	}

	return d.findings, nil
}
