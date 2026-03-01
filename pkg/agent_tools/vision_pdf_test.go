package tools

import (
	"reflect"
	"testing"
)

func TestSelectImagesForOCR_ReturnsAllWhenUnderLimit(t *testing.T) {
	images := [][]byte{{1, 2}, {3}, {4, 5, 6}}
	selected := selectImagesForOCR(images, 5)
	if !reflect.DeepEqual(selected, images) {
		t.Fatalf("expected original image set when under limit")
	}
}

func TestSelectImagesForOCR_ChoosesLargestAndPreservesOriginalOrder(t *testing.T) {
	images := [][]byte{
		make([]byte, 5),  // index 0
		make([]byte, 20), // index 1
		make([]byte, 8),  // index 2
		make([]byte, 18), // index 3
		make([]byte, 1),  // index 4
	}

	selected := selectImagesForOCR(images, 2)
	if len(selected) != 2 {
		t.Fatalf("expected 2 selected images, got %d", len(selected))
	}

	expected := [][]byte{images[1], images[3]}
	if !reflect.DeepEqual(selected, expected) {
		t.Fatalf("unexpected selected images")
	}
}
