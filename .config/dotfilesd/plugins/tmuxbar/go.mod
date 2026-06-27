module plugins/tmuxbar

go 1.26.3

replace (
	dotfilesd => /home/manu343726/dotfilesd
	plugins/resources => ../resources
)

require (
	dotfilesd v0.0.0
	plugins/resources v0.0.0
)
