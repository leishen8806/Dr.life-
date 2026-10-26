package gateway

import "testing"

func TestIdentityPermissionLevels(t *testing.T) {
	viewer := Identity{Permissions: map[string]string{"aud": "view"}}
	if !viewer.HasPermission("aud", "view") || viewer.HasPermission("aud", "edit") {
		t.Fatal("view permission was not enforced")
	}
	editor := Identity{Permissions: map[string]string{"aud": "edit"}}
	if !editor.HasPermission("aud", "view") || !editor.HasPermission("aud", "edit") {
		t.Fatal("edit permission must include view")
	}
	if (Identity{}).HasPermission("aud", "view") {
		t.Fatal("missing permission must fail closed")
	}
}
