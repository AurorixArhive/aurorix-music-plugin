package qqsearch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// 搜索模式靠 num 触发; 没有 songmid 的结果要滤掉。
func TestSearchAgainstStubAPI(t *testing.T) {
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		_, _ = w.Write([]byte(`{"code":0,"msg":"获取成功","data":[
			{"song":"晴天","singer":"周杰伦","cover":"https://y.gtimg.cn/c.jpg","mid":"0039MnYb0qxYhV"},
			{"song":"没 mid","singer":"x","cover":"","mid":""}
		]}`))
	}))
	defer server.Close()

	oldAPI := searchAPI
	searchAPI = server.URL
	defer func() { searchAPI = oldAPI }()

	found, err := Search(context.Background(), "晴天", 3)
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if gotQuery.Get("msg") != "晴天" || gotQuery.Get("num") != "3" {
		t.Errorf("请求参数不对: %v", gotQuery)
	}
	if len(found) != 1 {
		t.Fatalf("脏数据没滤掉, 拿到 %d 条: %+v", len(found), found)
	}
	if found[0].ID != "0039MnYb0qxYhV" || found[0].Title != "晴天" || found[0].Artist != "周杰伦" {
		t.Errorf("字段解析不对: %+v", found[0])
	}
	if found[0].Platform != "qq" {
		t.Errorf("音源标错了: %+v", found[0])
	}
}

func TestSearchReportsUpstreamFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":1,"msg":"搜索失败","data":[]}`))
	}))
	defer server.Close()

	oldAPI := searchAPI
	searchAPI = server.URL
	defer func() { searchAPI = oldAPI }()

	if _, err := Search(context.Background(), "查不到的东西", 5); err == nil {
		t.Fatal("上游给空列表时该报错")
	}
}
