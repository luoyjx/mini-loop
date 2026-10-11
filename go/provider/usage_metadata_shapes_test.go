package provider

import "testing"

func TestUsageMetadataShapesMatchActualSDKDirectAndStreamStages(t *testing.T) {
	assertUsageCountsFixture(t, "../testdata/python-provider-usage-metadata.json", 191)
}
