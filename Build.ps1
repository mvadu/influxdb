[CmdletBinding()]
param (
	#File paths and extensions
	[parameter(Mandatory=$true)]
	[string]$version= $null,
	[string]$branch= $null,
	[string]$commit = $null,
	[switch] $getLatest,
	[switch] $cleanOldBuild,
	[switch]$h, 
	[switch]$help 

)

begin{
	function print_usage() { 
	   write-host @" 
	   Build Mode: Builds Windows executable for Influx DB
	   Setup Mode: Download and install Golang on Windows. It sets the GOROOT environment variable and adds GOROOT\bin to the PATH environment variable. 
	  
	 Usage: 
	   $SCRIPT -version 0.11 -branch master -commit sha1 -getLatest  -cleanOldBuild
	 Options: 
	   -h | -help 
		 Print the help menu. 
		--------------------------------------------------------------
		For Build Mode
		--------------------------------------------------------------
		-version
			InfluxDB version being built, required
		-branch
			Branch Name, gets the current branch name by default
		-commit
			Commit SHA1 hash, gets the latest commit has by default
		-getLatest
			Pull latest code from origin, false by default
		-cleanOldBuild
			Run `go clean ./...` before build, true by default
"@ 
	} 

	if ($help -or $h) { 
	  print_usage 
	  exit 0 
	} 

	Function Go-Clean
	{
		$exclude = @('gdm.exe')
		go clean ./... 
		(Get-ChildItem -Path ("$($env:GOPATH)\bin") -Exclude $exclude -ErrorAction Ignore ).Fullname -like "*.exe" | Remove-Item
	}



	Function Get-GO-Version 
	{
		$v  = (go version)  -match "go version go([0-9].+) "
		return $($matches[1])
	}

	Function Get-Git-Branch
	{
		$branch=git rev-parse --abbrev-ref HEAD
		if (-NOT $? ) {
			write-host "Unable to retrieve current branch -- aborting"
			exit 1
		}
		return $branch
	}

	Function Get-Git-Commit
	{
		$commit=git rev-parse HEAD
		if (-NOT $? ) {
			write-host "Unable to retrieve commit -- aborting"
			exit 1
		}
		return $commit
	}


	Function Go-Get-Gdm
	{
		if(-not $($env:PATH).Contains("$env:GOPATH\bin;")){
			$env:PATH += ";$($env:GOPATH)\bin;"
			write-host $env:PATH
		}
		
		$pwd = Get-Location
		if(Test-Path $env:GOPATH\bin\gdm.exe){
			write-host "gdm installed"
			return 0
		}
		write-host "installing gdm"
		
		go get github.com/sparrc/gdm
		
		if(Test-Path $env:GOPATH\bin\gdm.exe){
			return 0
		}
		
		return -1
	}
	write-host "Setting working directory to $PSScriptRoot"
	Push-Location -Path $PSScriptRoot
}



process{

	if($branch -eq ""){
		$branch=Get-Git-Branch
	}
	
	if($commit -eq ""){
		$commit= Get-Git-Commit
	}
	
	if (-NOT $? ) {
		write-host "Unable to retrieve current commit -- aborting"
        exit 1
    }
	
	
	if($getLatest){
		$stagecount=(git status --porcelain | Measure-Object -Line).Lines
		
		if ( $stagecount -gt 0 ) {
			write-host "Uncommitted changes found, stash them"
			$STASH= git stash create -a
			if ( -NOT $?) {
				write-host "WARNING: failed to stash uncommitted local changes"
			}
			git reset --hard
		}
		
		write-host "Getting Latest code.." 			
		go get -u -f -d ./...
		if ( -NOT $?) {
			write-host "WARNING: failed to 'go get' packages."
		}
		
		if ( $stagecount -gt 0 ) {
			git stash apply $STASH
			if ( -NOT $?) { #and apply previous uncommited local changes
				write-host "WARNING: failed to restore uncommited local changes"
			}
		}
	}
	
	$date=((get-date).ToUniversalTime()).ToString('yyyy-MM-ddTHH:mm:ss+0000')
  
	write-host "Getting gdm dependency manager.." 			
	$r = Go-Get-Gdm 
	if($r -ne 0){
		write-error "Unable to get gdm"
	}
	
	git checkout $branch # go get switches to master, so ensure we're back.
	write-host "Restore dependencies.." 	
	gdm restore

	if($CleanOldBuild){
		write-host "Clean old build"
		Go-Clean
	}
	
	write-host "Create new build"
	$goVersion = Get-Go-Version
	write-host "Using go$goversion"	
	
	if($goversion -like "1.4*")
	{
		$ldflags = "-X main.version $version -X main.branch $branch -X main.commit $commit -X main.buildTime $date"
	}
	else
	{
		$ldflags = "-X main.version=$version -X main.branch=$branch -X main.commit=$commit -X main.buildTime=$date"
	}
	
	
	write-host "build flags: $ldflags"
	
	
	go install -ldflags="$ldflags" ./...
	
	
    if ( -NOT $?) {
        write-host "Build failed, unable to create package -- aborting"
        exit 1
    }
    write-host "Build completed successfully."

}
end{
	write-host "Restore PWD"
	Pop-Location -PassThru
}