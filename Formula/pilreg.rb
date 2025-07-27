class Pilreg < Formula
  desc "Tool to enumerate images and collect metadata from Docker registries"
  homepage "https://github.com/antitree/go-pillage-registries"
  url "https://github.com/antitree/go-pillage-registries/archive/2eb3684b0e99467b877905a5fed3c3011a9a6033.tar.gz"
  sha256 "338ab16a028f33e6343a4ba60ff71a6e74293a5a44f7c3b634f720e3738db612"
  license "MIT"
  head "https://github.com/antitree/go-pillage-registries.git", branch: "main"

  depends_on "go" => :build

  def install
    ldflags = "-s -w -X 'main.version=2.0' -X 'main.buildDate=#{Time.now}'"
    system "go", "build", *std_go_args(ldflags: ldflags), "./cmd/pilreg"
  end

  test do
    assert_match "pilreg", shell_output("#{bin}/pilreg --help")
  end
end
