package codex

import "testing"

func TestIsInternalTitleGeneratorPayload(t *testing.T) {
	if !IsInternalTitleGeneratorPayload(`{"title":"创建鹈鹕骑车SVG动画","description":"生成一个HTML文件，用SVG绘制鹈鹕骑自行车的2D动画"}`) {
		t.Fatal("expected Codex Desktop JSON title helper payload to be recognized")
	}
	if IsInternalTitleGeneratorPayload(`{"title":"demo","extra_field":123}`) {
		t.Fatal("unexpected keys must not be treated as internal title payload")
	}
	if IsInternalTitleGeneratorPayload("已创建：pelican-bicycle.html，直接用浏览器打开即可观看。") {
		t.Fatal("normal assistant markdown must not be filtered")
	}
}
