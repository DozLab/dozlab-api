-- Each lab names the init image with its VM rootfs (dozlab-rootfs-manager dozlab-init-<lab>).
-- NULL uses the controller's default init image.
ALTER TABLE labs ADD COLUMN IF NOT EXISTS init_image VARCHAR(255);
