//go:build !windows

package cheevos

import "lancast/internal/retro/libretro"

// Client is Windows only, as the desktop player that hosts cores is
// (internal/retro/libretro). Open says so; the methods exist so that code
// holding a *Client builds everywhere, and do nothing.
type Client struct{}

// Open is Windows only.
func Open(path string, h Handler) (*Client, error) { return nil, ErrUnsupported }

func (c *Client) Close()                                     {}
func (c *Client) UserAgentClause() string                    { return "" }
func (c *Client) Respond(id uint32, body []byte, status int) {}
func (c *Client) Login(username, token string) error         { return ErrUnsupported }
func (c *Client) SetMemory(console Console, mmap []libretro.MemoryDescriptor, system, save []byte) bool {
	return false
}
func (c *Client) LoadGame(console Console, path string, data []byte) {}
func (c *Client) UnloadGame()                                        {}
func (c *Client) DoFrame()                                           {}
func (c *Client) Idle()                                              {}
func (c *Client) Reset()                                             {}
func (c *Client) Progress() ([]byte, error)                          { return nil, ErrUnsupported }
func (c *Client) RestoreProgress(b []byte) error                     { return ErrUnsupported }
func (c *Client) Hash(console Console, path string, data []byte) (string, bool) {
	return "", false
}
func (c *Client) MemoryRegions() int                      { return 0 }
func (c *Client) ReadMemory(address uint32, n int) []byte { return nil }
