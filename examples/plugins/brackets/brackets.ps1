# Selkit plugin: the text comes on stdin, the action in SELKIT_ACTION; the result goes to stdout.
# Exit code 3 means "does not fit this text" (the message goes to stderr).
# Windows PowerShell reads a script without a BOM in the ANSI code page, so keep this file ASCII.
[Console]::InputEncoding = [Text.Encoding]::UTF8
[Console]::OutputEncoding = New-Object Text.UTF8Encoding $false
$text = [Console]::In.ReadToEnd()

switch ($env:SELKIT_ACTION) {
    'round' { $out = '(' + $text + ')' }
    'square' { $out = '[' + $text + ']' }
    'curly' { $out = '{' + $text + '}' }
    'unwrap' {
        # The first and last characters, spaces around the text aside, have to be a matching pair.
        $t = $text.Trim()
        $pair = if ($t.Length -ge 2) { [string]$t[0] + $t[-1] } else { '' }
        if (@('()', '[]', '{}') -notcontains $pair) {
            [Console]::Error.WriteLine('The text is not in brackets')
            exit 3
        }
        $out = $t.Substring(1, $t.Length - 2)
    }
    default { [Console]::Error.WriteLine("Unknown action $env:SELKIT_ACTION"); exit 1 }
}
[Console]::Out.Write($out)
