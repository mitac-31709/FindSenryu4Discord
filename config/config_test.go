package config

import "testing"

func TestSetDefaults_YomeMax(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{name: "未設定", in: 0, want: 10},
		{name: "負数", in: -1, want: 10},
		{name: "明示値", in: 5, want: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{Discord: DiscordConfig{YomeMax: tt.in}}
			setDefaults(c)
			if c.Discord.YomeMax != tt.want {
				t.Errorf("YomeMax = %d, want %d", c.Discord.YomeMax, tt.want)
			}
		})
	}
}

func TestSetDefaults_TankaReaction(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "未設定", in: "", want: "☝️"},
		{name: "明示値", in: "👆", want: "👆"},
		{name: "カスタム名", in: "tanka", want: "tanka"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{Discord: DiscordConfig{TankaReaction: tt.in}}
			setDefaults(c)
			if c.Discord.TankaReaction != tt.want {
				t.Errorf("TankaReaction = %q, want %q", c.Discord.TankaReaction, tt.want)
			}
		})
	}
}
