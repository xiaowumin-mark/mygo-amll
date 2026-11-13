package main

const effectRootShader = `package main
func LayerEffect(pos vec2) vec4 {
 s:=layerAt(pos); b:=backdropAt(pos)
 c:=min(vec4(1),s+b)
 if b.a>=.999999 { return c }
 // Invert the renderer's following source-over, avoiding a second
 // backdrop contribution when the backdrop itself is translucent.
 a:=clamp((c.a-b.a)/max(1-b.a,.000001),0,1)
 return vec4(clamp(c.rgb-(1-a)*b.rgb,vec3(0),vec3(a)),a)
}`

// These original Kage shaders implement premultiplied alpha, Gaussian filtering
// and the mask/transform mathematics of AMLL f32285864056587c4afa59d310208b0aa829cb53.
// They are not copied GLSL. The reference animation sources are AGPL-3.0-only:
// packages/core/src/lyric-player/dom/lyric-line/animation/{mask,float,emphasize}.
const effectWordShader = `package main
var RasterScale float
var Padding float
var WordSize vec2
var WordFloat float
var Mask vec4
var Foreground vec4
var BlackOffset vec2
var UnderRect vec4
var UnderOrigin vec2
var OverRect vec4
var OverOrigin vec2
var Count int
var GlyphRects [64]vec4
var GlyphOrigins [64]vec4
var GlyphBoxes [64]vec4
var Poses [64]vec4
var GlowRects [64]vec4
var GlowOrigins [64]vec4
func over(a, b vec4) vec4 { return a+b*(1-a.a) }
func inside(p vec2, r vec4) bool {
 return r.z>0 && r.w>0 && p.x>=r.x && p.y>=r.y && p.x<r.x+r.z && p.y<r.y+r.w
}
func glyph(q vec2, r vec4, origin vec2) vec4 {
 p:=r.xy+(q-origin)*RasterScale
 if !inside(p,r) { return vec4(0) }
 w:=image0Sample(p)
 b:=vec4(0); bp:=p-BlackOffset
 if bp.x>=0 && bp.y>=0 && bp.x<image1Size().x && bp.y<image1Size().y { b=image1Sample(bp) }
 return vec4(b.rgb+(w.rgb-b.rgb)*Foreground.rgb,w.a)*Foreground.a
}
func Fragment(dst vec4, pos vec2) vec4 {
 q:=pos/RasterScale-vec2(Padding)
 q.y-=WordFloat
 if q.x < -Padding || q.x>=WordSize.x+Padding || q.y < -Padding || q.y>=WordSize.y+Padding { return vec4(0) }
 c:=glyph(q,UnderRect,UnderOrigin)
 for i:=0; i<64; i++ {
  if i>=Count { break }
  b:=GlyphBoxes[i]
  o:=b.xy+b.zw*.5
  s:=max(Poses[i].x,.001)
  local:=(q-o)/s-Poses[i].yz+o-b.xy
  gr:=GlowRects[i]
  uv:=gr.xy+(local-GlowOrigins[i].xy)*RasterScale
  if inside(uv,gr) {
   a:=image2Sample(uv).a*GlowOrigins[i].z
   c=over(vec4(a,a,a,a),c)
  }
  c=over(glyph(local,GlyphRects[i],GlyphOrigins[i].xy),c)
 }
 c=over(glyph(q,OverRect,OverOrigin),c)
 m:=mix(Mask.z,Mask.w,clamp((q.x-Mask.x)/max(Mask.y-Mask.x,.00001),0,1))
 return c*m
}`

// Composite is evaluated before filtering. Bright and dim are different inputs:
// the dim clone never receives the original's word/character animations.
const effectCompositeShader = `package main
var SourceRect vec4
var DimRect vec4
var AnnotationRect vec4
var DimOrigin vec2
var AnnotationOrigin vec2
var RasterScale float
var Foreground vec4
var BlackOffset vec2
var BrightOpacity float
var DimOpacity float
var SubOpacity float
var Opacity float
var Sigma float
var Kernel [193]float
var KernelRadius int
var KernelTotal float
var Axis vec2
var BlurOnly float
var ContentScale float
var ContentPivot vec2
var ContentClip vec4
func inside(p vec2,r vec4) bool { return r.z>0 && r.w>0 && p.x>=r.x && p.y>=r.y && p.x<r.x+r.z && p.y<r.y+r.w }
func staticPixel(q vec2,r vec4,origin vec2) vec4 {
 p:=r.xy+q-origin
 if !inside(p,r) { return vec4(0) }
 w:=image1Sample(p); b:=vec4(0); bp:=p-BlackOffset
 if bp.x>=0 && bp.y>=0 && bp.x<image2Size().x && bp.y<image2Size().y { b=image2Sample(bp) }
 return vec4(b.rgb+(w.rgb-b.rgb)*Foreground.rgb,w.a)*Foreground.a
}
func sample(q vec2) vec4 {
 if ContentScale>0 { q=(q-ContentPivot)/ContentScale+ContentPivot }
 // CSS paint containment clips the unscaled line before its parent filter.
 // A second Gaussian pass samples already-clipped pixels and must keep blur.
 if BlurOnly<=0 && ContentClip.z>0 && ContentClip.w>0 && !inside(q,ContentClip) { return vec4(0) }
 if !inside(q,SourceRect) { return vec4(0) }
 c:=image0Sample(q)
 if BlurOnly>0 { return c }
 d:=staticPixel(q,DimRect,DimOrigin)
 c=min(vec4(1),c*BrightOpacity+d*DimOpacity)
 a:=staticPixel(q,AnnotationRect,AnnotationOrigin)*SubOpacity
 return a+c*(1-a.a)
}
func Fragment(dst vec4,pos vec2) vec4 {
 q:=pos*RasterScale
 if Sigma<=.00001 { return sample(q)*Opacity }
 radius:=KernelRadius; c:=vec4(0)
 for j:=0; j<=384; j++ {
  i:=j-radius
  if i>radius { break }
  k:=i; if k<0 { k=-k }; w:=Kernel[k]
  c+=sample(q+Axis*float(i))*w
 }
 return c/max(KernelTotal,.00001)*Opacity
}`

const effectGaussianShader = `package main
var SourceRect vec4
var SourceOrigin vec2
var Axis vec2
var Sigma float
var Kernel [193]float
var KernelRadius int
var KernelTotal float
var AlphaOnly float
var RasterScale float
var Opacity float
func sample(p vec2) vec4 {
 if p.x<SourceRect.x || p.y<SourceRect.y || p.x>=SourceRect.x+SourceRect.z || p.y>=SourceRect.y+SourceRect.w { return vec4(0) }
 c:=image0Sample(p)
 if AlphaOnly>0 { return vec4(c.a,c.a,c.a,c.a) }
 return c
}
func Fragment(dst vec4,pos vec2) vec4 {
 p:=SourceOrigin+pos*RasterScale
 if Sigma<=.00001 { return sample(p)*Opacity }
  radius:=KernelRadius; c:=vec4(0)
  for j:=0; j<=384; j++ {
   i:=j-radius
   if i>radius { break }
   k:=i; if k<0 { k=-k }; w:=Kernel[k]
   c+=sample(p+Axis*float(i))*w
  }
 return c/max(KernelTotal,.00001)*Opacity
}`
