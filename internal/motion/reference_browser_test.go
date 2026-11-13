package motion

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// This opt-in browser probe checks Web Animations composition rules separately
// from the original-TypeScript keyframe oracle. It opens no visible window and
// uses a temporary browser profile, never the user's running profile.
func TestReferenceWebAnimationsComposition(t *testing.T) {
	chrome := os.Getenv("AMLL_CHROME")
	if chrome == "" {
		t.Skip("set AMLL_CHROME to an installed Chromium executable for the isolated WAAPI composition probe")
	}
	temp := t.TempDir()
	file := filepath.Join(temp, "probe.html")
	if err := os.WriteFile(file, []byte(referenceBrowserProbe), 0600); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.ToSlash(file)
	if !strings.HasPrefix(filePath, "/") {
		filePath = "/" + filePath
	}
	uri := (&url.URL{Scheme: "file", Path: filePath}).String()
	command := exec.Command(chrome, "--headless", "--disable-gpu", "--no-first-run", "--no-default-browser-check", "--user-data-dir="+filepath.Join(temp, "profile"), "--dump-dom", uri)
	data, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start, end := strings.Index(text, "<body>"), strings.LastIndex(text, "</body>")
	if start < 0 || end <= start {
		t.Fatalf("browser probe returned no body: %s", data)
	}
	var result struct {
		Additive []float64
		Implicit []struct {
			Scale, X, Y float64
			Shadow      string
		}
		Underlying string
		Mask       []string
		Reverse    []float64
	}
	if err := json.Unmarshal([]byte(text[start+len("<body>"):end]), &result); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(result.Additive) != "[1.2 12 60]" {
		t.Fatalf("extra float is not composed inside glow scale: %v", result.Additive)
	}
	if len(result.Implicit) != 3 || result.Implicit[0].Scale != 1 || result.Implicit[0].X != 0 ||
		result.Implicit[1].Scale != 1.1 || result.Implicit[1].X != 5.5 || result.Implicit[1].Y != 11 ||
		result.Implicit[1].Shadow != "rgba(255, 255, 255, 0.25) 0px 0px 15px" {
		t.Fatalf("implicit neutral frame or first shadow interpolation differs: %+v", result.Implicit)
	}
	if result.Underlying != "0% 0%" || len(result.Mask) != 4 ||
		result.Mask[1] != "calc(0% - 10px) 0%" || result.Mask[2] != "0% 0%" || result.Mask[3] != "0% 0%" {
		t.Fatalf("mask implicit terminal frame does not return to its underlying position: %+v", result)
	}
	if fmt.Sprint(result.Reverse) != "[1300 100 1000 1300]" {
		t.Fatalf("negative-rate play auto-rewind boundaries differ: %v", result.Reverse)
	}
	t.Log("Chromium WAAPI: missing offset-0 is neutral; extra float is scaled; first shadow radius interpolates; mask terminal returns to underlying 0%; reverse play auto-rewinds at zero/past-end")
}

const referenceBrowserProbe = `<!doctype html><body><script>
const result={};
const matrix='matrix3d(1.2,0,0,0,0,1.2,0,0,0,0,1.2,0,0,0,0,1)';
const element=()=>{const el=document.createElement('div');el.style.cssText='width:100px;height:100px;font-size:100px';document.body.append(el);return el;};
const el=element();
const glow=el.animate([{transform:matrix+' translate(10px,20px)'},{transform:matrix+' translate(10px,20px)'}],{duration:1000,fill:'both',composite:'replace'});glow.pause();glow.currentTime=500;
const extra=el.animate([{transform:'translateY(30px)'},{transform:'translateY(30px)'}],{duration:1000,fill:'both',composite:'add'});extra.pause();extra.currentTime=500;
const composed=new DOMMatrix(getComputedStyle(el).transform);result.Additive=[composed.a,composed.e,composed.f];
const e=element();
const a=e.animate([{offset:1/32,transform:matrix+' translate(10px,20px)',textShadow:'0 0 30px rgba(255,255,255,.5)'},{offset:1,transform:matrix+' translate(10px,20px)',textShadow:'0 0 30px rgba(255,255,255,.5)'}],{duration:1000,fill:'both'});a.pause();
result.Implicit=[0,1000/64,1000/32].map(t=>{a.currentTime=t;const s=getComputedStyle(e),m=new DOMMatrix(s.transform);return {Scale:m.a,X:m.e,Y:m.f,Shadow:s.textShadow};});
const mask=element();mask.style.maskImage='linear-gradient(white,transparent)';mask.style.maskSize='200px 100%';
result.Underlying=getComputedStyle(mask).maskPosition;
const m=mask.animate([{offset:0,maskPosition:'-120px 0px'},{offset:.5,maskPosition:'-20px 0px'}],{duration:1000,fill:'both'});m.pause();
result.Mask=[500,750,1000,1200].map(t=>{m.currentTime=t;return getComputedStyle(mask).maskPosition;});
result.Reverse=[0,100,1000,2000].map(t=>{const a=el.animate([{opacity:0},{opacity:1}],{duration:1000,delay:300,fill:'both'});a.pause();a.currentTime=t;a.playbackRate=-1;a.play();const current=a.currentTime;a.cancel();return current;});
document.body.textContent=JSON.stringify(result);
</script>`
