$pattern = "feature\('([A-Z_]+)'\)"
$flags = @{}
Get-ChildItem -Path 'd:\LLM\project\WALL-AI\open-claudecode-go\opensource\claude-code-main\claude-code-main\src' -Filter '*.ts' -Recurse | ForEach-Object {
    $content = Get-Content $_.FullName -Raw
    if ($content) {
        $ms = [regex]::Matches($content, $pattern)
        foreach ($m in $ms) {
            $flags[$m.Groups[1].Value] = $true
        }
    }
}
$flags.Keys | Sort-Object
