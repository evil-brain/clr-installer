// Copyright © 2019 Intel Corporation
//
// SPDX-License-Identifier: GPL-3.0-only

package isoutils

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"text/template"

	"github.com/clearlinux/clr-installer/args"
	"github.com/clearlinux/clr-installer/cmd"
	"github.com/clearlinux/clr-installer/log"
	"github.com/clearlinux/clr-installer/model"
	"github.com/clearlinux/clr-installer/progress"
	"github.com/clearlinux/clr-installer/swupd"
	"github.com/clearlinux/clr-installer/utils"
)

type Paths int

const (
	clr_efi     Paths = iota + 0
	clr_img_efi       // Location to mount the EFI partition in the passed-in img file
	clr_initrd
	clr_rootfs
	clr_cdroot
)

var (
	tmpPaths = make([]string, 5)
)

func mkTmpDirs() error {
	msg := "Creating directory trees"
	prg := progress.NewLoop(msg)
	log.Info(msg)
	var err error

	tmpPaths[clr_efi], err = ioutil.TempDir("", "clr_efi-")
	if err != nil {
		prg.Failure()
		return (err)
	}
	tmpPaths[clr_initrd], err = ioutil.TempDir("", "clr_initrd-")
	if err != nil {
		prg.Failure()
		return (err)
	}
	tmpPaths[clr_cdroot], err = ioutil.TempDir("", "clr_cdroot-")
	if err != nil {
		prg.Failure()
		return (err)
	}

	/* Create specific directories for the new cd's root */
	for _, d := range []string{
		tmpPaths[clr_cdroot] + "/isolinux",
		tmpPaths[clr_cdroot] + "/EFI",
		tmpPaths[clr_cdroot] + "/images",
		tmpPaths[clr_cdroot] + "/kernel",
	} {
		if _, err := os.Stat(d); os.IsNotExist(err) {
			os.Mkdir(d, os.ModePerm)
		}
	}

	prg.Success()
	return err
}

func mkRootfs() error {
	msg := "making rootfs squashfs"
	prg := progress.NewLoop(msg)
	log.Info(msg)

	/* TODO: This takes a long time to run, it'd be nice to see it's output as it's running */
	args := []string{
		"mksquashfs",
		tmpPaths[clr_rootfs],
		tmpPaths[clr_cdroot] + "/images/rootfs.img",
		"-comp",
		"lzo",
		"-e",
		"boot/",
		"-e",
		"proc/",
		"-e",
		"sys/",
		"-e",
		"dev/",
		"-e",
		"run/",
	}
	err := cmd.RunAndLog(args...)
	if err != nil {
		prg.Failure()
		return err
	}
	prg.Success()
	return err
}

func mkInitrd(version string, model *model.SystemInstall) error {
	msg := "Installing the base system for initrd"
	prg := progress.NewLoop(msg)
	log.Info(msg)

	var err error
	options := args.Args{
		SwupdMirror:             model.SwupdMirror,
		SwupdStateDir:           tmpPaths[clr_initrd] + "/var/lib/swupd/",
		SwupdStateClean:         true,
		SwupdFormat:             "staging",
		SwupdSkipDiskSpaceCheck: true,
	}
	sw := swupd.New(tmpPaths[clr_initrd], options)

	/* Should install the overridden CoreBundles above (eg. os-core only) */
	if err := sw.Verify(version, model.SwupdMirror, true); err != nil {
		prg.Failure()
		return err
	}

	prg.Success()
	return err
}

func mkInitrdInitScript(templatePath string) error {
	msg := "Creating and installing init script to initrd"
	prg := progress.NewLoop(msg)
	log.Info(msg)

	type Modules struct {
		Modules []string
	}
	mods := Modules{}

	//Modules to insmod during init, paths relative to the kernel folder
	modules := []string{
		"/kernel/fs/isofs/isofs.ko",
		"/kernel/drivers/cdrom/cdrom.ko",
		"/kernel/drivers/scsi/sr_mod.ko",
		"/kernel/fs/overlayfs/overlay.ko",
	}

	/* Find kernel, then break the name into kernelVersion */
	kernelGlob, err := filepath.Glob(tmpPaths[clr_rootfs] + "/lib/kernel/org.clearlinux.*")
	if err != nil || len(kernelGlob) != 1 {
		prg.Failure()
		log.Error("Failed to determine kernel revision or > 1 kernel found")
		return err
	}
	kernelTypeVersion := strings.SplitAfter(filepath.Base((kernelGlob[0])), "org.clearlinux.")[1]
	kernelType := strings.Split(kernelTypeVersion, ".")[0] //kernelType examples: native,kvm,lts2018,hyperv
	kernelVersion := strings.SplitAfter(kernelTypeVersion, kernelType+".")[1]

	/* Copy files to initrd, and add to mods so they're added to the init template */
	for _, i := range modules {
		rootfsModPath := tmpPaths[clr_rootfs] + "/usr/lib/modules/" + kernelVersion + "." + kernelType + i

		/* copy kernel module to initramfs */
		initrdModPath := filepath.Dir(tmpPaths[clr_initrd] + "/usr/lib/modules/" + kernelVersion + "." + kernelType + i)

		if _, err := os.Stat(initrdModPath); os.IsNotExist(err) {
			os.MkdirAll(initrdModPath, os.ModePerm)
		}

		err = utils.CopyFile(rootfsModPath, initrdModPath+"/"+filepath.Base(i))
		if err != nil {
			prg.Failure()
			return err
		}
		mods.Modules = append(mods.Modules, "/usr/lib/modules/"+kernelVersion+"."+kernelType+i)
	}

	tmpl, err := ioutil.ReadFile(templatePath + "/initrd_init_template")
	if err != nil {
		prg.Failure()
		log.Error("Failed to open: initrd template at %s\n", templatePath+"initrd_init_template")
		return err
	}

	t := template.New("Modules template")
	t, err = t.Parse(string(tmpl))
	if err != nil {
		prg.Failure()
		log.Error("Failed to parse init's template")
		return err
	}

	f, err := os.Create(tmpPaths[clr_initrd] + "/init")
	if err != nil {
		prg.Failure()
		log.Error("Failed to create init file for initrd!")
		return err
	}
	defer f.Close()

	err = t.Execute(f, mods)
	if err != nil {
		prg.Failure()
		log.Error("Failed to execute template filling")
		return err
	}

	/* Set correct owner and permissions on initrd's init */
	if err = os.Chown(tmpPaths[clr_initrd]+"/init", 0, 0); err != nil {
		prg.Failure()
		return err
	}
	if err = os.Chmod(tmpPaths[clr_initrd]+"/init", 0700); err != nil {
		prg.Failure()
		return err
	}

	prg.Success()
	return err
}

/* Build initrd image, and copy to the correct location */
func buildInitrdImage() error {
	msg := "Building initrd image"
	prg := progress.NewLoop(msg)
	log.Info(msg)

	// Determine executible's path so we can revert to it when this function ends
	ex, err := os.Executable()
	if err != nil {
		prg.Failure()
		return err
	}
	exPath := filepath.Dir(ex)

	/* find all files in the initrd path, create the initrd */
	/* The find command must return filenames without a path (eg, must be run in the current dir) */
	os.Chdir(tmpPaths[clr_initrd])

	args := "sudo find .| cpio -o -H newc | gzip >" + tmpPaths[clr_cdroot] + "/images/initrd.gz"
	_, err = exec.Command("bash", "-c", args).CombinedOutput()
	if err != nil {
		prg.Failure()
		return err
	}
	os.Chdir(exPath)
	prg.Success()
	return err
}

func mkEfiBoot() error {
	msg := "Building efiboot image"
	prg := progress.NewLoop(msg)
	log.Info(msg)

	cmds := [][]string{
		[]string{"fallocate", "-l", "800M", tmpPaths[clr_cdroot] + "/EFI/efiboot.img"},
		[]string{"mkfs.fat", "-n", "\"CLEAR_EFI\"", tmpPaths[clr_cdroot] + "/EFI/efiboot.img"},
		[]string{"mount", "-t", "vfat", "-o", "loop", tmpPaths[clr_cdroot] + "/EFI/efiboot.img", tmpPaths[clr_efi]},
		[]string{"cp", "-pr", tmpPaths[clr_img_efi] + "/.", tmpPaths[clr_efi]},
	}

	for _, i := range cmds {
		err := cmd.RunAndLog(i...)
		if err != nil {
			prg.Failure()
			return err
		}
	}

	/* create dirs in new efi img */
	if _, err := os.Stat(tmpPaths[clr_efi] + "/EFI/systemd"); os.IsNotExist(err) {
		os.MkdirAll(tmpPaths[clr_efi]+"/EFI/systemd", os.ModePerm)
	}

	/* Modify loader/entries/Clear-linux-*, add initrd= line and remove ROOT= and rootwait from kernel command line options */
	entriesGlob, err := filepath.Glob(tmpPaths[clr_efi] + "/loader/entries/Clear-linux-*")
	if err != nil || len(entriesGlob) != 1 {
		prg.Failure()
		log.Error("Failed to modify efi entries file")
		return err
	}

	input, err := ioutil.ReadFile(entriesGlob[0])
	if err != nil {
		prg.Failure()
		log.Error("Failed to read EFI entries file")
		return err
	}

	/* Replace current options line with initrd information */
	lines := strings.Split(string(input), "\n")
	for i, line := range lines {
		if strings.Contains(line, "options") {
			lines[i] = "initrd /EFI/BOOT/initrd.gz"
		}
	}

	/* Pull kernel options from FS, add them to the buffer, write the file */
	optionsGlob, err := filepath.Glob(tmpPaths[clr_rootfs] + "/usr/lib/kernel/cmdline*")
	if err != nil || len(optionsGlob) > 1 { // Fail if there's >1 kernel(s)
		prg.Failure()
		log.Error("Failed to determine kernel boot params for initrd")
		return err
	}
	optionsFile, err := ioutil.ReadFile(optionsGlob[0])
	if err != nil {
		prg.Failure()
		log.Error("Cannot read kernel options file from rootfs")
		return err
	}
	lines = append(lines, "options "+string(optionsFile))

	err = ioutil.WriteFile(entriesGlob[0], []byte(strings.Join(lines, "\n")), 0644)
	if err != nil {
		prg.Failure()
		log.Error("Failed to write kernel boot paramaters file")
		return err
	}

	/* Copy all required files to efiboot.img and finally unmount efiboot.img */
	paths := [][]string{
		[]string{tmpPaths[clr_cdroot] + "/images/initrd.gz", tmpPaths[clr_efi] + "/EFI/Boot/initrd.gz"},
		[]string{tmpPaths[clr_img_efi] + "/EFI/BOOT/BOOTX64.EFI", tmpPaths[clr_efi] + "/EFI/systemd/systemd-bootx64.efi"},
	}

	for _, i := range paths {
		err = utils.CopyFile(i[0], i[1])
		if err != nil {
			prg.Failure()
			return err
		}
	}

	if err := syscall.Unmount(tmpPaths[clr_efi], syscall.MNT_FORCE|syscall.MNT_DETACH); err != nil {
		prg.Failure()
		return err
	}

	prg.Success()
	return err
}

func mkLegacyBoot(templatePath string) error {
	msg := "Setting up BIOS boot with isolinux"
	prg := progress.NewLoop(msg)
	log.Info(msg)

	type BootConf struct {
		Options string
	}
	bc := BootConf{}

	/* Find kernel path so we can copy the kernel later */
	kernelGlob, err := filepath.Glob(tmpPaths[clr_rootfs] + "/lib/kernel/org.clearlinux.*")
	if err != nil || len(kernelGlob) != 1 {
		prg.Failure()
		return err
	}
	kernelPath := kernelGlob[0]

	paths := [][]string{
		[]string{"/usr/share/syslinux/isohdpfx.bin", tmpPaths[clr_cdroot] + "/isolinux/isohdpfx.bin"},
		[]string{"/usr/share/syslinux/isolinux.bin", tmpPaths[clr_cdroot] + "/isolinux/isolinux.bin"},
		[]string{"/usr/share/syslinux/ldlinux.c32", tmpPaths[clr_cdroot] + "/isolinux/ldlinux.c32"},
		[]string{"/usr/share/syslinux/menu.c32", tmpPaths[clr_cdroot] + "/isolinux/menu.c32"},
		[]string{"/usr/share/syslinux/libutil.c32", tmpPaths[clr_cdroot] + "/isolinux/libutil.c32"},
		[]string{kernelPath, tmpPaths[clr_cdroot] + "/kernel/kernel.xz"},
	}

	for _, i := range paths {
		err = utils.CopyFile(i[0], i[1])
		if err != nil {
			prg.Failure()
			return err
		}
	}

	/* Create the 'boot.txt' file for isolinux */
	bootFile, err := os.Create(tmpPaths[clr_cdroot] + "/isolinux/boot.txt")
	if err != nil {
		prg.Failure()
		return err
	}
	defer bootFile.Close()
	bootFile.WriteString("\n\nClear Linux OS for Intel Architecture\n")

	/* Find the (kernel boot) options file, load it into bc.Options */
	optionsGlob, err := filepath.Glob(tmpPaths[clr_rootfs] + "/lib/kernel/cmdline*")
	if err != nil || len(optionsGlob) > 1 { // Fail if there's >1 match
		prg.Failure()
		log.Error("Failed to determine boot options for kernel")
		return err
	}
	optionsFile, err := ioutil.ReadFile(optionsGlob[0])
	if err != nil {
		prg.Failure()
		log.Error("Failed to read options file from rootfs")
		return err
	}
	bc.Options = string(optionsFile)

	/* Fill boot options in isolinux.cfg */
	tmpl, err := ioutil.ReadFile(templatePath + "/isolinux.cfg.template")
	if err != nil {
		prg.Failure()
		log.Error("Failed to find template")
		return err
	}

	t := template.New("Modules template")
	t, err = t.Parse(string(tmpl))
	if err != nil {
		prg.Failure()
		log.Error("Failed to parse template.")
		return err
	}

	f, err := os.Create(tmpPaths[clr_cdroot] + "/isolinux/isolinux.cfg")
	if err != nil {
		prg.Failure()
		log.Error("Failed to create isolinux.cfg on cd root!")
		return err
	}
	defer f.Close()

	err = t.Execute(f, bc)
	if err != nil {
		prg.Failure()
		log.Error("Failed to execute template filling")
		return err
	}

	return err
}

func packageIso(version string, imgName string) error {
	msg := "Building ISO"
	prg := progress.NewLoop(msg)
	log.Info(msg)

	args := []string{
		"xorriso", "-as", "mkisofs",
		"-o", "clear-" + version + "-" + imgName + ".iso",
		"-V", "CLR_ISO",
		"-isohybrid-mbr", tmpPaths[clr_cdroot] + "/isolinux/isohdpfx.bin",
		"-c", "isolinux/boot.cat", "-b", "isolinux/isolinux.bin",

		"-no-emul-boot", "-boot-load-size", "4", "-boot-info-table",
		"-eltorito-alt-boot", "-e", "EFI/efiboot.img", "-no-emul-boot",
		"-isohybrid-gpt-basdat", tmpPaths[clr_cdroot],
	}
	err := cmd.RunAndLog(args...)
	if err != nil {
		prg.Failure()
		return err
	}

	prg.Success()
	return err

}

func cleanup() {
	msg := "Cleaning up from ISO creation"
	prg := progress.NewLoop(msg)
	log.Info(msg)
	var err error

	/* In case something fails during mkEfiBoot, check and umount clr_img_efi */
	if err = syscall.Unmount(tmpPaths[clr_efi], syscall.MNT_FORCE|syscall.MNT_DETACH); err != nil {
		//Failed to unmount, usually the normal case but could bee umount actually failed.
	}

	/* Remove all directories in /tmp/clr_* */
	for _, d := range tmpPaths {
		if d == tmpPaths[clr_rootfs] || d == tmpPaths[clr_img_efi] { //both these paths are handled by clr-installer
			continue
		}
		err = os.RemoveAll(d)
		if err != nil {
			log.Warning("Failed to remove dir: %s", d)
		}
	}
	prg.Success()
}

func MakeIso(rootDir string, imgName string, model *model.SystemInstall) error {
	tmpPaths[clr_rootfs] = rootDir
	tmpPaths[clr_img_efi] = rootDir + "/boot"
	var err error

	// Determine version from the root filesystem
	version, err := ioutil.ReadFile(rootDir + "/usr/share/clear/version")
	if err != nil {
		return err
	}

	if err = mkTmpDirs(); err != nil {
		return err
	}
	defer cleanup()

	if err = mkRootfs(); err != nil {
		return err
	}

	if err = mkInitrd(string(version), model); err != nil {
		return err
	}

	if err = mkInitrdInitScript("/home/blovin/go/src/github.com/clearlinux/clr-installer/isoutils/"); err != nil { //TODO: Remove hardcoded path
		return err
	}

	if err = buildInitrdImage(); err != nil {
		return err
	}
	if err = mkEfiBoot(); err != nil {
		return err
	}
	err = mkLegacyBoot("/home/blovin/go/src/github.com/clearlinux/clr-installer/isoutils/")
	if err != nil {
		return err
	}
	if err = packageIso(string(version), imgName); err != nil {
		return err
	}

	return err
}
