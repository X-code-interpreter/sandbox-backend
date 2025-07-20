package build

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/KarpelesLab/reflink"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/config"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/utils"
	"github.com/docker/docker/client"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type TemplateManagerConfig struct {
	Subnet            config.IPNet    `toml:"subnet"`
	KernelDebugOutput bool            `toml:"kernel_debug_output"`
	RootfsBuildMode   RootfsBuildMode `toml:"rootfs_build_mode"`
	TemplateToBuild   string          `toml:"template_id"`
	EnvdPath          string          `toml:"envd_path"`

	HypervisorBinaryPath string `toml:"-"`
	DataRoot             string `toml:"-"`
	config.VMTemplate    `toml:"-"`
}

type RootfsBuildMode string

const (
	Normal RootfsBuildMode = "normal"
	// build only rootfs
	BuildRootfsOnly = "build-rootfs-only"
	// skip build rootfs
	SkipBuildRootfs = "skip-build-rootfs"
)

func (m *RootfsBuildMode) UnmarshalText(data []byte) error {
	switch RootfsBuildMode(data) {
	case Normal, BuildRootfsOnly, SkipBuildRootfs:
		*m = RootfsBuildMode(data)
		return nil
	default:
		return fmt.Errorf("invalid rootfs build mode: %s", data)
	}
}

var ErrInvalidRootfsBuildMode = errors.New("invalid rootfs build mode")

func (c *TemplateManagerConfig) CachedRootfsPath() string {
	return filepath.Join(c.TemplateDir(c.DataRoot), "cache", consts.RootfsName)
}

func (c *TemplateManagerConfig) CachedWritableRootfsPath() string {
	return filepath.Join(c.TemplateDir(c.DataRoot), "cache", consts.WritableFsName)
}

func (c *TemplateManagerConfig) Validate() error {
	if err := c.VMTemplate.Validate(); err != nil {
		return err
	}
	if c.DataRoot == "" {
		return fmt.Errorf("data_root cannot be empty")
	}
	if _, err := exec.LookPath(c.HypervisorBinaryPath); err != nil {
		return fmt.Errorf("hypervisor binary %s not found: %w", c.HypervisorBinaryPath, err)
	}
	if _, err := exec.LookPath(c.EnvdPath); err != nil {
		return fmt.Errorf("envd binary %s not found: %w", c.EnvdPath, err)
	}
	return nil
}

// Dump the VMTemplate to [VmTemplate.EnvDirPath].
func (c *TemplateManagerConfig) dumpVMTemplate(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "dump-vm-template")
	defer childSpan.End()

	f, err := os.Create(c.TemplateFilePath(c.DataRoot))
	if err != nil {
		errMsg := fmt.Errorf("error creating template file: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	defer f.Close()

	enc := toml.NewEncoder(f)
	if err = enc.Encode(c.VMTemplate); err != nil {
		errMsg := fmt.Errorf("error encode template: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	return nil
}

func (c *TemplateManagerConfig) initialize(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "initialize")
	defer childSpan.End()

	var err error
	defer func() {
		if err != nil {
			c.Cleanup(childCtx, tracer)
		}
	}()

	// PrivateKernelPath includes: TemplateDir, PrivateDir and PrivateKernelPath
	err = utils.CreateFileAndDirIfNotExists(c.PrivateKernelPath(c.DataRoot), 0o644, 0o755)
	if err != nil {
		errMsg := fmt.Errorf("error initialize private kernel path: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	// Later, we use os.Rename to move PrivateDir into TemplateImgDir.
	// golang go.Rename does not allow dst to be an empty directory, so
	// we do not create TemplateImgDir here.

	telemetry.ReportEvent(childCtx, "created tmp build dir")
	return nil
}

func (c *TemplateManagerConfig) Cleanup(ctx context.Context, tracer trace.Tracer) {
	childCtx, childSpan := tracer.Start(ctx, "cleanup")
	defer childSpan.End()

	err := os.RemoveAll(c.PrivateDir(c.DataRoot))
	if err != nil {
		errMsg := fmt.Errorf("error cleaning up env files: %w", err)
		telemetry.ReportError(childCtx, errMsg)
	} else {
		telemetry.ReportEvent(childCtx, "cleaned up env files")
	}
}

func (c *TemplateManagerConfig) moveSnapshot() error {
	type snapshotFile struct {
		base    string
		dirPath string
	}
	var (
		snapshotFiles []snapshotFile
		tmpFileDir    = c.PrivateDir(c.DataRoot)
	)

	switch c.VmmType {
	case config.FIRECRACKER:
		snapshotFiles = append(snapshotFiles, snapshotFile{
			base:    consts.FcSnapfileName,
			dirPath: tmpFileDir,
		}, snapshotFile{
			base:    consts.FcMemfileName,
			dirPath: tmpFileDir,
		},
		)
	case config.CLOUDHYPERVISOR:
		for _, base := range consts.ChSnapshotFiles {
			snapshotFiles = append(snapshotFiles, snapshotFile{
				base:    base,
				dirPath: tmpFileDir,
			})
		}
	default:
		return config.InvalidVmmType
	}
	for _, file := range snapshotFiles {
		if err := os.Rename(
			filepath.Join(file.dirPath, file.base),
			filepath.Join(c.TemplateDir(c.DataRoot), file.base),
		); err != nil {
			return err
		}
	}
	return nil
}

// moveRootfsForCache will be used by build mode BuildRootfsOnly.
func (c *TemplateManagerConfig) moveRootfsForCache(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "move-rootfs-for-build-rootfs-only")
	defer childSpan.End()

	targetPath := c.CachedRootfsPath()
	if err := utils.CreateDirAllIfNotExists(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("error creating cache dir for rootfs: %w", err)
	}
	if err := os.Rename(c.PrivateRootfsPath(c.DataRoot), targetPath); err != nil {
		telemetry.ReportCriticalError(childCtx, err)
		return err
	}
	telemetry.ReportEvent(childCtx, "moved rootfs")

	if c.Overlay {
		targetPath := c.CachedWritableRootfsPath()
		if err := os.Rename(c.PrivateWritableRootfsPath(c.DataRoot), targetPath); err != nil {
			telemetry.ReportCriticalError(childCtx, err)
			return err
		}
		telemetry.ReportEvent(childCtx, "moved writable rootfs")
	}
	return nil
}

// prepareRootfsFromPreviousBuild will be used by build mode SkipBuildRootfs.
func (c *TemplateManagerConfig) prepareRootfsFromCache(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "prepare-rootfs-from-cache")
	defer childSpan.End()
	paths := []struct{ src, dst string }{
		{
			c.CachedRootfsPath(),
			c.PrivateRootfsPath(c.DataRoot),
		},
	}
	if c.Overlay {
		paths = append(paths, struct{ src, dst string }{
			c.CachedWritableRootfsPath(),
			c.PrivateWritableRootfsPath(c.DataRoot),
		})
	}
	for _, path := range paths {
		// reflink auto will fallback to copy if reflink is not supported
		if err := reflink.Auto(path.src, path.dst); err != nil {
			return err
		}
		telemetry.ReportEvent(childCtx, "copied rootfs",
			attribute.String("src", path.src),
			attribute.String("dst", path.dst),
		)
	}
	return nil
}

func (c *TemplateManagerConfig) MoveToTemplateImgDir(ctx context.Context, tracer trace.Tracer) error {
	_, childSpan := tracer.Start(ctx, "move-to-env-dir")
	defer childSpan.End()

	src := c.PrivateDir(c.DataRoot)
	dst := c.TemplateImgDir(c.DataRoot)

	// go rename does not allow dst to be an emopty directory
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	return os.Rename(src, dst)

	// if err := c.moveSnapshot(); err != nil {
	// 	telemetry.ReportCriticalError(childCtx, err)
	// 	return err
	// }
	// telemetry.ReportEvent(childCtx, "move snapshot files")
	//
	// return c.moveRootfsToEnvDir(ctx, tracer)
}

func (c *TemplateManagerConfig) BuildTemplate(ctx context.Context, tracer trace.Tracer, docker *client.Client) error {
	childCtx, childSpan := tracer.Start(ctx, "build")
	defer childSpan.End()

	err := c.initialize(childCtx, tracer)
	if err != nil {
		errMsg := fmt.Errorf("error initializing directories for building env '%s' during build : %w", c.TemplateID, err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	defer c.Cleanup(childCtx, tracer)

	switch c.RootfsBuildMode {
	case Normal, BuildRootfsOnly:
		_, err = NewRootfs(childCtx, tracer, docker, c)
		if err != nil {
			errMsg := fmt.Errorf("error creating rootfs for env '%s' during build: %w", c.TemplateID, err)
			telemetry.ReportCriticalError(childCtx, errMsg)

			return errMsg
		}
	case SkipBuildRootfs:
		err = c.prepareRootfsFromCache(childCtx, tracer)
		if err != nil {
			errMsg := fmt.Errorf("error preparing rootfs from previous build for env '%s' during build: %w", c.TemplateID, err)
			telemetry.ReportCriticalError(childCtx, errMsg)

			return errMsg
		}
	default:
		return ErrInvalidRootfsBuildMode
	}

	if c.RootfsBuildMode == BuildRootfsOnly {
		return c.moveRootfsForCache(childCtx, tracer)
	}

	network, err := NewNetworkEnvForSnapshot(childCtx, tracer, c)
	if err != nil {
		errMsg := fmt.Errorf("error network setup for FC while building env '%s' during build: %w", c.TemplateID, err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	defer func() {
		ntErr := network.Cleanup(childCtx)
		if ntErr != nil {
			errMsg := fmt.Errorf("error removing network namespace: %w", ntErr)
			telemetry.ReportError(childCtx, errMsg)
		} else {
			telemetry.ReportEvent(childCtx, "removed network namespace")
		}
	}()

	_, err = NewSnapshot(childCtx, tracer, c, network)
	if err != nil {
		errMsg := fmt.Errorf("error snapshot for env '%s' during build: %w", c.TemplateID, err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	err = c.MoveToTemplateImgDir(childCtx, tracer)
	if err != nil {
		errMsg := fmt.Errorf("error moving images while building env '%s': %w", c.TemplateID, err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	err = c.dumpVMTemplate(childCtx, tracer)
	if err != nil {
		errMsg := fmt.Errorf("error dump template while building env '%s' : %w", c.TemplateID, err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	return nil
}

// api-socket of vm
func (c *TemplateManagerConfig) GetSocketPath() string {
	socketFileName := fmt.Sprintf("vmm-build-sock-%s.sock", c.TemplateID)
	return filepath.Join(os.TempDir(), socketFileName)
}

func ParseTemplateManagerConfig(configFile string) (*TemplateManagerConfig, error) {
	var (
		globalConfig struct {
			config.CommonConfig
			Templates          map[string]toml.Primitive `toml:"template"`
			TemplateManagerCfg toml.Primitive            `toml:"template_manager"`
		}
		tmConfig TemplateManagerConfig
		tConfig  config.VMTemplate
		err      error
	)

	// if not provided, try to get the default config file path
	if len(configFile) == 0 {
		configFile, err = config.GetConfigFilePath()
		if err != nil {
			return nil, err
		}
	}
	meta, err := toml.DecodeFile(configFile, &globalConfig)
	if err != nil {
		return nil, fmt.Errorf("error decoding runtime config: %w", err)
	}

	if err = meta.PrimitiveDecode(globalConfig.TemplateManagerCfg, &tmConfig); err != nil {
		return nil, fmt.Errorf("error decoding template manager: %w", err)
	}
	tmConfig.DataRoot = globalConfig.DataRoot

	templateName := tmConfig.TemplateToBuild
	if templatePrimitive, ok := globalConfig.Templates[templateName]; ok {
		if err = meta.PrimitiveDecode(templatePrimitive, &tConfig); err != nil {
			return nil, fmt.Errorf("error decoding template %s: %w", templateName, err)
		}
	} else {
		return nil, fmt.Errorf("template %s not found in config", templateName)
	}
	tConfig.TemplateID = templateName
	tmConfig.VMTemplate = tConfig
	switch tConfig.VmmType {
	case config.FIRECRACKER:
		tmConfig.HypervisorBinaryPath = globalConfig.CommonConfig.FCBinaryPath
	case config.CLOUDHYPERVISOR:
		tmConfig.HypervisorBinaryPath = globalConfig.CommonConfig.CHBinaryPath
	}

	tmConfig.setDefaultVal()
	// validate
	if err := tmConfig.Validate(); err != nil {
		return nil, fmt.Errorf("error validating template manager config: %w", err)
	}
	return &tmConfig, nil
}

func (c *TemplateManagerConfig) setDefaultVal() {
	// The default ipnet used by manager
	if c.Subnet.IPNet == nil {
		c.Subnet.IPNet = &net.IPNet{
			IP:   net.ParseIP("10.160.0.0"),
			Mask: net.CIDRMask(30, 32),
		}
	}

	if c.KernelVersion == "" {
		c.KernelVersion = consts.DefaultKernelVersion
	}
	if c.HypervisorBinaryPath == "" {
		c.HypervisorBinaryPath = "firecracker"
	}
}
