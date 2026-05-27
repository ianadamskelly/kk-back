package api

import "testing"

func TestValidServicePillarRequiresPillarForPublishedServices(t *testing.T) {
	for _, pillar := range []string{"brand_identity", "digital_platforms", "content_growth"} {
		if !validServicePillar(pillar, "published") {
			t.Fatalf("expected %q to be a valid published service pillar", pillar)
		}
	}
	if validServicePillar("", "published") {
		t.Fatal("expected an ungrouped published service to be rejected")
	}
	if validServicePillar("education", "published") {
		t.Fatal("expected an unknown service pillar to be rejected")
	}
}

func TestValidServicePillarAllowsUngroupedDraft(t *testing.T) {
	if !validServicePillar("", "draft") {
		t.Fatal("expected a draft service to be saved before pillar assignment")
	}
}
