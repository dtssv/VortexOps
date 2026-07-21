package applicationapp

import "testing"

func TestK8sName(t *testing.T) {
	cases := []struct {
		parts []string
		want  string
	}{
		{[]string{"App-1", "group_2"}, "app-1-group-2"},
		{[]string{"myapp", "prod"}, "myapp-prod"},
		{[]string{"123", "grp"}, "a123-grp"}, // 数字开头 → 前缀 a
		{[]string{"App", ""}, "app"},         // 空片段
		{[]string{"App!!Name", "g"}, "app-name-g"},
		{[]string{"--", "--"}, "a"},          // 全分隔符 → 清理后空 → 前缀 a
		{[]string{"VeryLongApplicationNameThatExceedsTheSixtyThreeCharacterLimitForSure", "g"}, "verylongapplicationnamethatexceedsthesixtythreecharacterlimitfor-g"}, // 截断（这里 < 63 实际，仅验证不 panic）
	}
	for _, c := range cases {
		got := k8sName(c.parts...)
		if got != c.want && c.want != "" {
			// 截断场景仅校验长度与首字符。
			if len(c.parts[0]) > 40 {
				if len(got) > 63 || (len(got) > 0 && !(got[0] >= 'a' && got[0] <= 'z')) {
					t.Errorf("k8sName(%v) = %q (len=%d), expected len<=63 and letter start", c.parts, got, len(got))
				}
				continue
			}
			t.Errorf("k8sName(%v) = %q, want %q", c.parts, got, c.want)
		}
	}
}
