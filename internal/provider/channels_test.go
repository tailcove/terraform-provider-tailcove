package provider

import (
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestMemberChannelsSendOnlyTheSwitch(t *testing.T) {
	c := channelsModel{
		Mail: &memberModel{Members: types.BoolValue(true)},
		Sms:  &memberModel{Members: types.BoolValue(true)},
	}

	want := map[string]any{
		"mail": map[string]any{"members": true},
		"sms":  map[string]any{"members": true},
	}
	if got := c.toAPI(); !reflect.DeepEqual(got, want) {
		t.Fatalf("toAPI() = %#v, want %#v", got, want)
	}
}

func TestMemberChannelsFollowTheAPI(t *testing.T) {
	c := channelsModel{Mail: &memberModel{Members: types.BoolValue(true)}, Sms: &memberModel{Members: types.BoolValue(true)}}

	c.refresh(map[string]any{"mail": map[string]any{"members": true}}, false)

	if c.Mail == nil || !c.Mail.Members.ValueBool() {
		t.Fatalf("mail should stay on, got %#v", c.Mail)
	}
	if c.Sms != nil {
		t.Fatalf("sms the API no longer has should be dropped, got %#v", c.Sms)
	}

	imported := channelsModel{}
	imported.refresh(map[string]any{"sms": map[string]any{"members": true}}, true)
	if imported.Sms == nil || !imported.Sms.Members.ValueBool() {
		t.Fatalf("an import should take the sms channel, got %#v", imported.Sms)
	}
}
