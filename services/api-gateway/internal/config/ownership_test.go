package config

import "testing"

func TestConfigurationRoutesUseContentOwner(t *testing.T) {
	t.Setenv("ROUTE_0", "")
	routes := loadRoutes()
	for _, path := range []string{"/api/admin/v1/defects", "/api/v1/admin/pitfalls"} {
		found := false
		for _, route := range routes {
			if route.PathPrefix == path {
				found = true
				if route.TargetURL != "http://localhost:8082" || !route.Auth || route.Strip {
					t.Fatalf("configuration ownership/auth changed: %+v", route)
				}
			}
		}
		if !found {
			t.Fatalf("missing route %s", path)
		}
	}
}
