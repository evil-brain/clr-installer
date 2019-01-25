// Copyright © 2018-2019 Intel Corporation
//
// SPDX-License-Identifier: GPL-3.0-only

package pages

import (
	"github.com/clearlinux/clr-installer/model"
	"github.com/clearlinux/clr-installer/telemetry"
	"github.com/gotk3/gotk3/gtk"
)

// Telemetry is a simple page to help with Telemetry settings
type Telemetry struct {
	model      *model.SystemInstall
	controller Controller
	box        *gtk.Box
	check      *gtk.CheckButton
	didConfirm bool
}

// NewTelemetryPage returns a new TelemetryPage
func NewTelemetryPage(controller Controller, model *model.SystemInstall) (Page, error) {
	box, err := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 0)
	if err != nil {
		return nil, err
	}
	box.SetVAlign(gtk.ALIGN_CENTER)

	lab, err := gtk.LabelNew(telemetry.HelpMarkdown)
	if err != nil {
		return nil, err
	}
	lab.SetUseMarkup(true)
	box.PackStart(lab, false, false, 0)

	check, err := gtk.CheckButtonNewWithLabel("Enable telemetry")
	if err != nil {
		return nil, err
	}
	check.SetHAlign(gtk.ALIGN_CENTER)
	box.PackStart(check, false, false, 0)

	return &Telemetry{
		controller: controller,
		model:      model,
		box:        box,
		check:      check,
		didConfirm: false,
	}, nil
}

// IsRequired will return true as we always need a Telemetry
func (t *Telemetry) IsRequired() bool {
	return true
}

// IsDone checks if all the steps are completed
func (t *Telemetry) IsDone() bool {
	return t.didConfirm
}

// GetID returns the ID for this page
func (t *Telemetry) GetID() int {
	return PageIDTelemetry
}

// GetIcon returns the icon for this page
func (t *Telemetry) GetIcon() string {
	return "network-transmit-receive"
}

// GetRootWidget returns the root embeddable widget for this page
func (t *Telemetry) GetRootWidget() gtk.IWidget {
	return t.box
}

// GetSummary will return the summary for this page
func (t *Telemetry) GetSummary() string {
	return "Telemetry"
}

// GetTitle will return the title for this page
func (t *Telemetry) GetTitle() string {
	return telemetry.Title
}

// StoreChanges will store this pages changes into the model
func (t *Telemetry) StoreChanges() {
	t.didConfirm = true
	t.model.EnableTelemetry(t.check.GetActive())
}

// ResetChanges will reset this page to match the model
func (t *Telemetry) ResetChanges() {
	t.controller.SetButtonState(ButtonConfirm, true)
	t.check.SetActive(t.model.IsTelemetryEnabled())
}

// GetConfiguredValue returns our current config
func (t *Telemetry) GetConfiguredValue() string {
	if t.model.IsTelemetryEnabled() {
		return "Enabled"
	}
	return "Disabled"
}