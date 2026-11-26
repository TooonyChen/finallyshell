package app

import (
	"io/fs"
	"path"

	"finallyshell/internal/remotefs"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

type filePermissions struct {
	loaded   bool
	original fs.FileMode
	mode     fs.FileMode
}

func (a *App) askFilePermissions(s *Session, p string) {
	if s.sftp == nil || p == "" || p == "/" {
		return
	}
	d := &fileDialog{sess: s, op: opChmod, dir: path.Dir(p), old: path.Base(p)}
	a.fileDlg = d
	a.loadFilePermissions(d)
}

func (a *App) loadFilePermissions(d *fileDialog) {
	sc := d.sess.sftp
	if sc == nil || d.busy {
		return
	}
	d.busy, d.err = true, nil
	p := path.Join(d.dir, d.old)
	go func() {
		mode, err := remotefs.FilePermissions(sc, p)
		a.post(func() {
			if a.fileDlg != d {
				return
			}
			d.busy, d.err = false, err
			d.perms = filePermissions{loaded: err == nil, original: mode.Perm(), mode: mode.Perm()}
		})
	}()
}

func (a *App) filePermissionsView(c *ui.Context, d *fileDialog) {
	ui.Column(c).Width(290).Gap(10).Children(func() {
		ui.Text(c, locales.ChangePermissionsTitle()).FontSize(16).Bold().AlignSelf(ui.Center)
		ui.Text(c, d.old).SingleLine()
		if d.perms.loaded {
			filePermissionGroups(c, d)
		}
		if d.err != nil {
			ui.Text(c, fileOpErrorText(d.err)).TextColor(theme.Danger).MaxLines(3)
			if !d.perms.loaded && ui.Button(c, locales.Retry()).AlignSelf(ui.Start).Clicked() {
				a.loadFilePermissions(d)
			}
		}
		if d.busy {
			ui.Spinner(c).AlignSelf(ui.Center)
		}
		ui.Row(c).Gap(10).Justify(ui.Center).Children(func() {
			unchanged := !d.perms.loaded || d.perms.mode == d.perms.original
			if ui.PrimaryButton(c, locales.Ok()).Width(108).Disabled(d.busy || unchanged).Clicked() {
				a.runFileDialog(d)
			}
			if ui.Button(c, locales.Cancel()).Width(108).Clicked() {
				a.closeFileDialog(d)
			}
		})
	})
}

func filePermissionGroups(c *ui.Context, d *fileDialog) {
	groups := [...]string{locales.PermissionOwner(), locales.PermissionGroup(), locales.PermissionOthers()}
	actions := [...]string{locales.PermissionRead(), locales.PermissionWrite(), locales.PermissionExecute()}
	for i, group := range groups {
		ui.Row(c).Gap(8).Padding(11, 10, 7).Margin(4, 0, 0, 0).AlignItems(ui.Center).
			Border(1, theme.Border).Radius(3).Role(ui.RoleGroup).Label(group).Children(func() {
			ui.Text(c, group).Bold().Padding(0, 4).Background(theme.Background).
				Attach(ui.AnchorTopStart, ui.AnchorStart).Left(8)
			for j, action := range actions {
				bit := fs.FileMode(0o400 >> (i*len(actions) + j))
				on := d.perms.mode&bit != 0
				if ui.Checkbox(c, &on, action).Width(88).Disabled(d.busy).Changed() {
					d.perms.mode ^= bit
					d.err = nil
				}
			}
		})
	}
}
