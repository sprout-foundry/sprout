package providercatalog

import "testing"

func TestEmbeddedCatalogLoads(t *testing.T) {
	catalog := Current()
	if len(catalog.Providers) == 0 {
		t.Fatal("expected embedded provider catalog to contain providers")
	}

	if provider, ok := FindProvider("zai"); !ok || provider.ID != "zai" {
		t.Fatal("expected zai provider to be present in catalog")
	}
}
