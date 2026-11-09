// Package timeline indexes lyric activity and visual time states independently
// of a UI or audio player. The host supplies absolute playback positions,
// including seeks, and owns reusable query buffers. FollowController separately
// controls when a view may apply automatic focus after manual scrolling.
package timeline
