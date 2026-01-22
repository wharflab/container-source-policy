Gem::Specification.new do |spec|
  spec.name          = "container-source-policy"
  spec.version       = "0.1.0"
  spec.authors       = ["Konstantin Vyatkin"]
  spec.email         = ["tino@vtkn.io"]

  spec.summary       = "Generate Buildx container source policy file for a given Dockerfile"
  spec.homepage      = "https://github.com/tinovyatkin/container-source-policy"
  spec.post_install_message = "container-source-policy installed! Run 'container-source-policy --help' to see usage."

  spec.bindir        = "bin"
  spec.executables   << "container-source-policy"
  spec.require_paths = ["lib"]

  spec.files = %w(
    lib/container-source-policy.rb
    bin/container-source-policy
  ) + `find libexec/ -executable -type f -print0`.split("\x0")

  spec.licenses = ['MIT']
end
