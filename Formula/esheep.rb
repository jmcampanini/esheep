class Esheep < Formula
  desc "Manage local Agent Skills for Claude Code, Pi, and Codex"
  homepage "https://github.com/jmcampanini/esheep"
  license "MIT"
  head "https://github.com/jmcampanini/esheep.git", branch: "main"

  depends_on "go" => :build

  def install
    ldflags = %W[
      -s -w
      -X github.com/jmcampanini/esheep/cmd.Version=#{version}
    ]
    system "go", "build", "-buildvcs=false", *std_go_args(ldflags:)
    generate_completions_from_executable(bin/"esheep", "completion")
  end

  test do
    assert_match "esheep version HEAD-", shell_output("#{bin}/esheep --version")
  end
end
