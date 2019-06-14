package graylog

import (
	"reflect"
	"testing"
)

func TestWriterAddIgnoreSuffix(t *testing.T) {
	w := new(Writer)
	suffixes := []string{"dir1/file1.go", "dir2/file2.go"}

	w.addIgnoreSuffix(suffixes...)

	if len(w.ignoreSuffix) != len(suffixes) {
		t.Errorf("ingnoreSuffix: expected len %d, got %d", len(suffixes), len(w.ignoreSuffix))
	}

	if !reflect.DeepEqual(w.ignoreSuffix, suffixes) {
		t.Errorf("ignoreSuffix: expected %v, got %v", w.ignoreSuffix, suffixes)
	}
}
