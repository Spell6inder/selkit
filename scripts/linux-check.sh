#!/bin/sh
# Runs the Linux build with real programs: rofi on X11, fzf in a terminal, fuzzel on Wayland (headless sway).
# Everything happens inside a throwaway container. From the repository root, in a POSIX shell:
#
#   GOOS=linux go build -o var/bin/selkit-linux ./cmd/selkit
#   docker run --rm --cap-add SYS_NICE -v "$PWD:/src" alpine:3.20 sh /src/scripts/linux-check.sh
#
# (Git Bash on Windows: prefix with MSYS_NO_PATHCONV=1 and use "$(pwd -W)" instead of "$PWD".)
# sway needs SYS_NICE to start. The keys are pressed by xdotool, wtype and script.
W=/src/var/bin/selkit-linux
failed=0
check() { # name, got, want
	if [ "$2" = "$3" ]; then
		echo "ok    $1"
	else
		echo "FAIL  $1: got '$2', want '$3'"
		failed=1
	fi
}

apk add -q xvfb xclip xdotool rofi fzf util-linux-misc sway wl-clipboard wtype fuzzel ttf-dejavu >/dev/null 2>&1
mkdir -p ~/.config/selkit/plugins
cp -r /src/examples/plugins/brackets ~/.config/selkit/plugins/
export LANG=C

Xvfb :1 -screen 0 1024x768x24 >/dev/null 2>&1 &
sleep 1
export DISPLAY=:1

# The first menu item is the transliteration.
printf 'Щука' | xclip -i -selection primary
$W menu </dev/null 2>/dev/null &
P=$!
sleep 2
xdotool key Return
wait $P
check "rofi: clipboard" "$(xclip -o -selection clipboard)" "Shchuka"
check "rofi: primary" "$(xclip -o -selection primary)" "Shchuka"
check "rofi: last action" "$(cat ~/.config/selkit/last-action)" "translit"

printf 'Shchuka' | xclip -i -selection primary
$W repeat </dev/null 2>/dev/null
check "repeat goes back" "$(xclip -o -selection clipboard)" "Щука"

# A plugin without preview runs when chosen.
printf 'Hello' | xclip -i -selection primary
$W menu </dev/null 2>/dev/null &
P=$!
sleep 2
xdotool type 'Round brackets'
sleep 1
xdotool key Return
wait $P
check "plugin without preview" "$(xclip -o -selection clipboard)" "(Hello)"

# fzf draws on the terminal. The check runs inside it, because the xclip left serving the clipboard ends with it.
printf 'Hello' | xclip -i -selection primary
(sleep 3; printf '\r'; sleep 2) | TERM=xterm script -qfc "$W menu 2>/dev/null; xclip -o -selection clipboard > /tmp/fzf.out" /dev/null >/dev/null 2>&1
check "fzf in a terminal" "$(cat /tmp/fzf.out)" "Гелло"

printf 'Hello' | xclip -i -selection primary
got=$($W menu -picker nosuchprogram </dev/null 2>&1)
case "$got" in
*nosuchprogram*) echo "ok    a menu program that cannot start is reported" ;;
*) echo "FAIL  menu program error: $got"; failed=1 ;;
esac

pkill Xvfb
sleep 1
got=$($W menu </dev/null 2>&1)
case "$got" in
*display*) echo "ok    a missing display is reported" ;;
*) echo "FAIL  missing display: $got"; failed=1 ;;
esac
unset DISPLAY

export LANG=C.UTF-8 XDG_RUNTIME_DIR=/tmp/xdg WLR_BACKENDS=headless WLR_LIBINPUT_NO_DEVICES=1 WLR_RENDERER=pixman
mkdir -p $XDG_RUNTIME_DIR
chmod 700 $XDG_RUNTIME_DIR
echo 'output HEADLESS-1 resolution 1024x768' >/tmp/sway.conf
sway -c /tmp/sway.conf >/tmp/sway.log 2>&1 &
sleep 3
WAYLAND_DISPLAY=$(ls $XDG_RUNTIME_DIR | grep '^wayland-[0-9]*$' | head -1)
export WAYLAND_DISPLAY
# Under headless sway, fuzzel sometimes takes the key from wtype as a dismissal, without Selkit too: try again.
for try in 1 2 3 4 5 6; do
	printf 'fooBar' | wl-copy --primary
	sleep 0.5
	$W menu </dev/null 2>/dev/null &
	P=$!
	sleep 2
	wtype -k Return
	wait $P
	[ "$(wl-paste --no-newline 2>/dev/null)" = "фооБар" ] && break
done
check "fuzzel: clipboard" "$(wl-paste --no-newline)" "фооБар"
check "fuzzel: primary" "$(wl-paste --primary --no-newline)" "фооБар"

[ $failed = 0 ] && echo "All passed" || echo "Some checks FAILED"
exit $failed
