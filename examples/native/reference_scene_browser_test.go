// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"encoding/json"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Optional browser rule oracle; it uses an isolated temporary profile and opens
// no visible window. It does not substitute for the real Native GPU checks.
func TestReferenceCSSAgainstChromium(t *testing.T) {
	chrome := os.Getenv("AMLL_CHROME")
	if chrome == "" {
		t.Skip("set AMLL_CHROME for the isolated CSS transition oracle")
	}
	temp := t.TempDir()
	file := filepath.Join(temp, "css.html")
	if err := os.WriteFile(file, []byte(referenceCSSProbe), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.ToSlash(file)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	uri := (&url.URL{Scheme: "file", Path: path}).String()
	data, err := exec.Command(chrome, "--headless", "--disable-gpu", "--no-first-run", "--no-default-browser-check", "--user-data-dir="+filepath.Join(temp, "profile"), "--dump-dom", uri).Output()
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start, end := strings.Index(text, "<body>"), strings.LastIndex(text, "</body>")
	if start < 0 || end <= start {
		t.Fatalf("missing result body: %s", data)
	}
	var oracle struct {
		Before, After, Start float64
		Duration             float64
		MountTransition      []float64
		HoverFilter          string
		HoverDuration        float64
	}
	if err := json.Unmarshal([]byte(text[start+len("<body>"):end]), &oracle); err != nil {
		t.Fatalf("browser JSON: %v: %s", err, data)
	}
	now := time.Unix(0, 0)
	var tween referenceTween
	tween.snap(0)
	tween.set(now, 1, 400*time.Millisecond, false)
	when := now.Add(200 * time.Millisecond)
	before := tween.sample(when)
	tween.set(when, 0, 400*time.Millisecond, false)
	if math.Abs(before-oracle.Before) > 1e-6 || math.Abs(tween.sample(when)-oracle.After) > 1e-6 || math.Abs(float64(tween.duration)/float64(time.Millisecond)-oracle.Duration) > 1e-5 {
		t.Fatalf("native transition=%g/%g duration%v browser=%+v", before, tween.sample(when), tween.duration, oracle)
	}
	if len(oracle.MountTransition) != 1 || oracle.MountTransition[0] != 400 {
		t.Fatalf("show/measure-before-style mount did not start its transition: %+v", oracle)
	}
	if oracle.HoverDuration != 400 || !strings.HasPrefix(oracle.HoverFilter, "blur(0.987") {
		t.Fatalf("important hover filter did not keep its CSS transition: %+v", oracle)
	}
	t.Logf("Chromium CSS: reversal starts at %.6f, shortened duration %.9fms; mounting/measurement starts a fresh 400ms transition", oracle.Before, oracle.Duration)
}

const referenceCSSProbe = `<!doctype html><body><script>
try {
const el=document.createElement('div');el.style.cssText='width:100px;height:20px;opacity:0;transition:opacity .4s ease';document.body.append(el);getComputedStyle(el).opacity;
el.style.opacity='1';getComputedStyle(el).opacity;const a=el.getAnimations()[0];a.pause();a.currentTime=200;const before=Number(getComputedStyle(el).opacity);
el.style.opacity='0';const after=Number(getComputedStyle(el).opacity);const b=el.getAnimations()[0];b.pause();b.currentTime=0;
const result={Before:before,After:after,Duration:b.effect.getTiming().duration,Start:Number(getComputedStyle(el).opacity)};
const mount=document.createElement('div');mount.style.cssText='width:100px;height:20px;transition:opacity .4s ease';document.body.append(mount);mount.getBoundingClientRect();mount.style.opacity='.85';getComputedStyle(mount).opacity;result.MountTransition=mount.getAnimations().map(x=>x.effect.getTiming().duration);
const css=document.createElement('style');css.textContent='.filtered.hovered{filter:unset!important}';document.head.append(css);const filter=document.createElement('div');filter.className='filtered';filter.style.cssText='width:20px;height:20px;filter:blur(5px);transition:filter .4s ease';document.body.append(filter);getComputedStyle(filter).filter;filter.classList.add('hovered');getComputedStyle(filter).filter;const f=filter.getAnimations()[0];f.pause();f.currentTime=200;result.HoverDuration=f.effect.getTiming().duration;result.HoverFilter=getComputedStyle(filter).filter;
document.body.textContent=JSON.stringify(result);
}catch(e){document.body.textContent=String(e.stack)}
</script>`
