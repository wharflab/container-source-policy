#!/usr/bin/env ruby

require "fileutils"
require "json"

VERSION = "0.1.0"

ROOT = File.join(__dir__, "..")
DIST = File.join(ROOT, "dist")

PYTHON_PLATFORMS = ["linux", "darwin", "windows", "freebsd"].product(["x86_64", "arm64"])

module Pack
  extend FileUtils

  module_function

  def prepare
    clean
    set_version
    put_additional_files
    put_binaries
  end

  def clean
    cd(__dir__)
    puts "Cleaning... "
    rm(Dir["npm/**/README.md"])
    rm(Dir["npm/**/container-source-policy*"].filter(&File.method(:file?)))
    system("git clean -fdX npm/ pypi/ rubygems/", exception: true)
    puts "done"
  end

  def set_version
    cd(__dir__)
    puts "Replacing version to #{VERSION} in packages"
    
    # Update NPM packages
    Dir["npm/**/package.json"].each do |package_json|
      replace_in_file(package_json, /"version": "[\d.]+"/, %{"version": "#{VERSION}"})
    end
    
    # Update main NPM package optional dependencies
    replace_in_file("npm/container-source-policy/package.json", 
                   /"(container-source-policy-.+)": "[\d.]+"/, 
                   %{"\\1": "#{VERSION}"})
    
    # Update PyPI version
    replace_in_file("pypi/pyproject.toml", /(version\s*=\s*)"[^"]+"/, %{\\1"#{VERSION}"})
    
    # Update Rubygems version
    replace_in_file("rubygems/container-source-policy.gemspec", /(spec\.version\s+=\s+)"[^"]+"/, %{\\1"#{VERSION}"})
  end

  def put_additional_files
    cd(__dir__)
    puts "Putting README... "
    Dir["npm/*"].each do |npm_dir|
      cp(File.join(ROOT, "README.md"), File.join(npm_dir, "README.md"), verbose: true)
    end
    puts "done"
  end

  def put_binaries
    cd(__dir__)
    puts "Putting binaries to packages..."
    
    # NPM binaries
    {
      "#{DIST}/container-source-policy_#{VERSION}_Linux_x86_64"        => "npm/container-source-policy-linux-x64/bin/container-source-policy",
      "#{DIST}/container-source-policy_#{VERSION}_Linux_arm64"         => "npm/container-source-policy-linux-arm64/bin/container-source-policy",
      "#{DIST}/container-source-policy_#{VERSION}_Freebsd_x86_64"      => "npm/container-source-policy-freebsd-x64/bin/container-source-policy",
      "#{DIST}/container-source-policy_#{VERSION}_Windows_x86_64.exe"  => "npm/container-source-policy-windows-x64/bin/container-source-policy.exe",
      "#{DIST}/container-source-policy_#{VERSION}_Windows_arm64.exe"   => "npm/container-source-policy-windows-arm64/bin/container-source-policy.exe",
      "#{DIST}/container-source-policy_#{VERSION}_MacOS_x86_64"        => "npm/container-source-policy-darwin-x64/bin/container-source-policy",
      "#{DIST}/container-source-policy_#{VERSION}_MacOS_arm64"         => "npm/container-source-policy-darwin-arm64/bin/container-source-policy",
    }.each do |(source, dest)|
      mkdir_p(File.dirname(dest))
      cp(source, dest, verbose: true)
    end

    # Rubygems binaries
    {
      "#{DIST}/container-source-policy_#{VERSION}_Linux_x86_64"        => "rubygems/libexec/container-source-policy-linux-x64/container-source-policy",
      "#{DIST}/container-source-policy_#{VERSION}_Linux_arm64"         => "rubygems/libexec/container-source-policy-linux-arm64/container-source-policy",
      "#{DIST}/container-source-policy_#{VERSION}_Freebsd_x86_64"      => "rubygems/libexec/container-source-policy-freebsd-x64/container-source-policy",
      "#{DIST}/container-source-policy_#{VERSION}_Windows_x86_64.exe"  => "rubygems/libexec/container-source-policy-windows-x64/container-source-policy.exe",
      "#{DIST}/container-source-policy_#{VERSION}_Windows_arm64.exe"   => "rubygems/libexec/container-source-policy-windows-arm64/container-source-policy.exe",
      "#{DIST}/container-source-policy_#{VERSION}_MacOS_x86_64"        => "rubygems/libexec/container-source-policy-darwin-x64/container-source-policy",
      "#{DIST}/container-source-policy_#{VERSION}_MacOS_arm64"         => "rubygems/libexec/container-source-policy-darwin-arm64/container-source-policy",
    }.each do |(source, dest)|
      mkdir_p(File.dirname(dest))
      cp(source, dest, verbose: true)
    end

    # PyPI binaries
    {
      "#{DIST}/container-source-policy_#{VERSION}_Linux_x86_64"        => "pypi/container_source_policy/bin/container-source-policy-linux-x86_64/container-source-policy",
      "#{DIST}/container-source-policy_#{VERSION}_Linux_arm64"         => "pypi/container_source_policy/bin/container-source-policy-linux-arm64/container-source-policy",
      "#{DIST}/container-source-policy_#{VERSION}_Freebsd_x86_64"      => "pypi/container_source_policy/bin/container-source-policy-freebsd-x86_64/container-source-policy",
      "#{DIST}/container-source-policy_#{VERSION}_Windows_x86_64.exe"  => "pypi/container_source_policy/bin/container-source-policy-windows-x86_64/container-source-policy.exe",
      "#{DIST}/container-source-policy_#{VERSION}_Windows_arm64.exe"   => "pypi/container_source_policy/bin/container-source-policy-windows-arm64/container-source-policy.exe",
      "#{DIST}/container-source-policy_#{VERSION}_MacOS_x86_64"        => "pypi/container_source_policy/bin/container-source-policy-darwin-x86_64/container-source-policy",
      "#{DIST}/container-source-policy_#{VERSION}_MacOS_arm64"         => "pypi/container_source_policy/bin/container-source-policy-darwin-arm64/container-source-policy",
    }.each do |(source, dest)|
      mkdir_p(File.dirname(dest))
      cp(source, dest, verbose: true)
    end

    puts "done"
  end

  def publish
    publish_pypi
    publish_npm
    publish_gem
  end

  def publish_npm
    puts "Publishing container-source-policy npm..."
    cd(File.join(__dir__, "npm"))
    Dir["container-source-policy*"].each do |package|
      puts "publishing #{package}"
      cd(File.join(__dir__, "npm", package))
      system("npm publish --access public", exception: true)
      cd(File.join(__dir__, "npm"))
    end
  end

  def publish_gem
    puts "Publishing to Rubygems..."
    cd(File.join(__dir__, "rubygems"))
    system("rake build", exception: true)
    system("gem push pkg/*.gem", exception: true)
  end

  def publish_pypi
    puts "Publishing to PyPI..."
    pypi_dir = File.join(__dir__, "pypi")

    PYTHON_PLATFORMS.each do |os, arch|
      puts "Building wheel for #{os}-#{arch}..."
      cd(pypi_dir)
      ENV["CSP_TARGET_PLATFORM"] = os
      ENV["CSP_TARGET_ARCH"] = arch
      system("uv build --wheel", exception: true)
    end

    puts "Uploading to PyPI..."
    system("uv publish", exception: true)
  end

  def replace_in_file(filepath, regexp, value)
    text = File.open(filepath, "r") do |f|
      f.read
    end
    text.gsub!(regexp, value)
    File.open(filepath, "w") do |f|
      f.write(text)
    end
  end
end

ARGV.each do |cmd|
  Pack.public_send(cmd)
end
