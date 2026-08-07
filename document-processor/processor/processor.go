package processor

import (
	"github.com/crawler-monorepo/internal/extractor"
)

// Processor re-exports CleanDocument and ProcessRawHTML for backward compatibility.
type Processor = extractor.CleanDocument
