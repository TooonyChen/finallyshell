//go:build !windows

package localterm

import "context"

func Available() bool                                          { return false }
func Elevated() (bool, error)                                  { return false, nil }
func Start(context.Context, []string, string) (Process, error) { return nil, ErrUnsupported }
func RunHelper([]string) (bool, int)                           { return false, 0 }
