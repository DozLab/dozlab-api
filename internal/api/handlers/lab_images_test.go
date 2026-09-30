package handlers

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"dozlab-backend/internal/models"
)

func TestSetLabImages(t *testing.T) {
	newSession := func() *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]interface{}{
			"spec": map[string]interface{}{"sessionId": "s1"},
		}}
	}
	k8s := "dozlab-init-k8s:1"
	empty := ""

	for _, tc := range []struct {
		name string
		lab  models.Lab
		want string // "" = no customImages
	}{
		{"lab with an init image", models.Lab{InitImage: &k8s}, k8s},
		{"lab without one uses the controller default", models.Lab{}, ""},
		{"empty init image uses the controller default", models.Lab{InitImage: &empty}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newSession()
			setLabImages(s, tc.lab)
			got, _, _ := unstructured.NestedString(s.Object, "spec", "customImages", "initImage")
			_, has, _ := unstructured.NestedMap(s.Object, "spec", "customImages")
			if got != tc.want || has != (tc.want != "") {
				t.Errorf("customImages.initImage = %q (present %v), want %q", got, has, tc.want)
			}
		})
	}
}
