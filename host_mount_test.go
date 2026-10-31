package main

import ocli "github.com/unixsuperhero/ochazuke/cli"

// mountHostPage mounts the commands page the way an external catalog host
// (mtm, nabe) does: the page spec as published in the catalog document, and
// no o-cmds detail kinds registered.
func mountHostPage(a *App) (*page, error) {
	return mountPageAs(a, func(doc ocli.CatalogDoc) ocli.CatalogPage {
		for _, p := range doc.Pages {
			if p.ID == pageID {
				return p
			}
		}
		return ocli.CatalogPage{}
	}, ocli.PageConfig{})
}
