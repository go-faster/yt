package mapreduce

import "github.com/go-faster/yt/skiff"

// jobState is transferred from the client to the job.
//
// NOTE: all fields must be public and support gob encoding.
type jobState struct {
	Job Job

	InputSkiffFormat *skiff.Format
}
