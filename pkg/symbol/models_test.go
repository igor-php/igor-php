package symbol

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsVendor(t *testing.T) {
	root := "/app"
	if os.PathSeparator == '\\' {
		root = "C:\\app"
	}

	tests := []struct {
		name     string
		filePath string
		expected bool
	}{
		{
			name:     "Project file",
			filePath: filepath.Join(root, "src/Service/MyService.php"),
			expected: false,
		},
		{
			name:     "Vendor file (relative style)",
			filePath: filepath.Join(root, "vendor/symfony/http-kernel/Kernel.php"),
			expected: true,
		},
		{
			name:     "Vendor file (absolute style outside root)",
			filePath: "/tmp/vendor/package/file.php",
			expected: true,
		},
		{
			name:     "File with vendor in name but not in path",
			filePath: filepath.Join(root, "src/Provider/VendorProvider.php"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := AuditStatus{FilePath: tt.filePath}
			if got := status.IsVendor(root); got != tt.expected {
				t.Errorf("AuditStatus.IsVendor() = %v, want %v (path: %s)", got, tt.expected, tt.filePath)
			}
		})
	}
}
