$ErrorActionPreference = 'Stop'

$packageName    = 'derpy'
$url64          = 'https://github.com/punkscience/derpy/releases/download/v1.0.0/derpy_1.0.0_windows_amd64.zip'
$url64arm       = 'https://github.com/punkscience/derpy/releases/download/v1.0.0/derpy_1.0.0_windows_arm64.zip'
$checksum64     = '7fcea3cf8565b56879e6a00b90956b6ff01e57de8ca7b5bf01235d0430e09873'
$checksum64arm  = 'a03193b0e74a3ec4ca580049a85d114c55a26dab39ae5f5a096a59f49e4f3041'
$checksumType   = 'sha256'

# Select arch from the environment (fast, no WMI dependency).
if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') {
    $url        = $url64arm
    $checksum   = $checksum64arm
} else {
    $url        = $url64
    $checksum   = $checksum64
}

$toolsDir = "$(Split-Path -Parent $MyInvocation.MyCommand.Definition)"

$packageArgs = @{
    packageName   = $packageName
    unzipLocation = $toolsDir
    url64bit      = $url
    checksum64    = $checksum
    checksumType64= $checksumType
}

Install-ChocolateyZipPackage @packageArgs

# --- System Media Transport Controls (Win+A) app identity --------------------
# Create a Start Menu shortcut that carries derpy's AppUserModelID. The Windows
# media control (Win+A) resolves the media session's AUMID against registered
# apps (Start Menu shortcuts); without one it shows "Unknown app". derpy itself
# sets the same AUMID at runtime via SetCurrentProcessExplicitAppUserModelID, so
# this shortcut makes the media card resolve the friendly name "Derpy".
try {
    $exe   = Join-Path $toolsDir 'derpy.exe'
    $aumid = 'PunkScience.Derpy'

    Add-Type -Namespace Derpy -Name Shortcut -MemberDefinition @'
[ComImport, Guid("00021401-0000-0000-C000-000000000046")] public class ShellLink {}
[ComImport, Guid("000214F9-0000-0000-C000-000000000046"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
public interface IShellLinkW {
  void GetPath(System.Text.StringBuilder f,int c,IntPtr d,uint fl); void GetIDList(out IntPtr p); void SetIDList(IntPtr p);
  void GetDescription(System.Text.StringBuilder n,int c); void SetDescription([MarshalAs(UnmanagedType.LPWStr)] string n);
  void GetWorkingDirectory(System.Text.StringBuilder d,int c); void SetWorkingDirectory([MarshalAs(UnmanagedType.LPWStr)] string d);
  void GetArguments(System.Text.StringBuilder a,int c); void SetArguments([MarshalAs(UnmanagedType.LPWStr)] string a);
  void GetHotkey(out short h); void SetHotkey(short h); void GetShowCmd(out int s); void SetShowCmd(int s);
  void GetIconLocation(System.Text.StringBuilder p,int c,out int i); void SetIconLocation([MarshalAs(UnmanagedType.LPWStr)] string p,int i);
  void SetRelativePath([MarshalAs(UnmanagedType.LPWStr)] string p,uint r); void Resolve(IntPtr h,uint fl);
  void SetPath([MarshalAs(UnmanagedType.LPWStr)] string f);
}
[ComImport, Guid("886d8eeb-8cf2-4446-8d02-cdba1dbdcf99"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
public interface IPropertyStore {
  void GetCount(out uint c); void GetAt(uint i,out PROPERTYKEY k);
  void GetValue(ref PROPERTYKEY k,out PROPVARIANT v); void SetValue(ref PROPERTYKEY k,ref PROPVARIANT v); void Commit();
}
[StructLayout(LayoutKind.Sequential)] public struct PROPERTYKEY { public Guid fmtid; public uint pid; }
[StructLayout(LayoutKind.Sequential)] public struct PROPVARIANT { public ushort vt; public ushort r1; public ushort r2; public ushort r3; public IntPtr p; public IntPtr p2; }
[DllImport("ole32.dll")] public static extern int PropVariantClear(ref PROPVARIANT pv);
public static void Create(string lnk,string target,string aumid){
  var link=(IShellLinkW)new ShellLink(); link.SetPath(target);
  var store=(IPropertyStore)link;
  var key=new PROPERTYKEY{ fmtid=new Guid("9F4C2855-9F79-4B39-A8D0-E1D42DE1D5F3"), pid=5 };
  var pv=new PROPVARIANT{ vt=31 /*VT_LPWSTR*/, p=Marshal.StringToCoTaskMemUni(aumid) };
  store.SetValue(ref key, ref pv); store.Commit(); PropVariantClear(ref pv);
  ((System.Runtime.InteropServices.ComTypes.IPersistFile)link).Save(lnk,true);
}
'@

    # Prefer the all-users Start Menu (elevated install); fall back to per-user.
    $candidates = @(
        (Join-Path $env:ProgramData 'Microsoft\Windows\Start Menu\Programs\Derpy.lnk'),
        (Join-Path $env:APPDATA     'Microsoft\Windows\Start Menu\Programs\Derpy.lnk')
    )
    $created = $false
    foreach ($lnk in $candidates) {
        try {
            [Derpy.Shortcut]::Create($lnk, $exe, $aumid)
            Write-Host "Created media-controls shortcut: $lnk"
            $created = $true
            break
        } catch { }
    }
    if (-not $created) {
        Write-Warning "Could not create media-controls shortcut; Win+A will show 'Unknown app'."
    }
} catch {
    Write-Warning "Media-controls shortcut step failed: $_"
}