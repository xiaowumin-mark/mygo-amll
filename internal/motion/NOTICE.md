# Reference motion sources

The Go reference spring, policies, stagger and word/character animation code is
adapted from Apple Music-like Lyrics, commit
`f32285864056587c4afa59d310208b0aa829cb53`, under AGPL-3.0-only. This project
distributes its adaptations under AGPL-3.0-only; see the repository LICENSE.

Source paths:

- `packages/core/src/utils/spring.ts` and `utils/derivative.ts`.
- `packages/core/src/lyric-player/base/spring.ts`, `base/index.ts` and `base/group.ts`.
- `packages/core/src/lyric-player/dom/animation/float/index.ts`.
- `packages/core/src/lyric-player/dom/animation/emphasize/index.ts`.
- `packages/core/src/lyric-player/dom/lyric-line.ts` animation transition methods
  and `dom/animation/mask/animator-web.ts` clock semantics.
- `packages/core/src/utils/matrix.ts`.

Changes: replace mutable JavaScript closures and Web Animations objects with Go
value state and retained keyframes; inject explicit UI/song time; separate
idempotent frame preparation from pure sampling; bound emphasis storage to 33
shared frames regardless of character count. Preserve reference finite
derivatives, its damping-ratio branch, queue ordering and queue quirks. These
sources do not contain a separate author copyright notice; none is invented here.

The original spring solver's notice is reproduced verbatim:

```text
/** MIT License github.com/pushkine/ */
```

Original solver: https://gist.github.com/pushkine/1b595fda102bec88e012c4e4c0cd6d1a
(`spring.ts`). The retrieved original and the fixed AMLL copy provide only that
MIT declaration, with no copyright year or additional copyright notice. The
standard MIT permission and warranty terms are included below without inventing
an author/year line for that source.

The Cardano cubic-bezier easing solver is adapted from **bezier-easing 3.0.1**,
`src/index.js`, https://github.com/gre/bezier-easing. Its source credits Gaëtan
Renaudeau (2014–2015) and the algebraic solver by Dmitry Baranovskiy
(http://dmitry.baranovskiy.com/bezier-easing.html). Its retrieved source SHA-256 is
`9d8228a3583b1afb96fc2d037d0bccc01afa4e247bab992899513fb85c922f01`.
The complete permission notice from that release's LICENSE follows:

```text
Copyright (c) 2014 Gaëtan Renaudeau

Permission is hereby granted, free of charge, to any person
obtaining a copy of this software and associated documentation
files (the "Software"), to deal in the Software without
restriction, including without limitation the rights to use,
copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the
Software is furnished to do so, subject to the following
conditions:

The above copyright notice and this permission notice shall be
included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES
OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT
HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR
OTHER DEALINGS IN THE SOFTWARE.
```

The optional Node reference tests execute the user's ignored fixed reference
checkout and a separately supplied bezier-easing source file. They do not make
the reference checkout, Node or an npm package a runtime dependency, fetch a
package during normal tests, or vendor unreviewed generated output.
